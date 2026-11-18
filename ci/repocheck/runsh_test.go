// runsh_test.go runs ci/run.sh against a stub `docker` placed first on PATH and
// checks the argv of the `docker run` it would have issued: the sibling runs as
// the caller's uid:gid, binds the HOST workspace path, mounts the named cache
// volumes, and forwards the caller's BOARD_* variables and nothing else.
package repocheck

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

const dockerStub = `#!/bin/sh
# Records the argv of "docker run", one argument per line, NUL-free.
case "$1" in
run)
    for a in "$@"; do printf '%s\n' "$a"; done > "$STUB_RUN_LOG"
    ;;
esac
exit 0
`

// runShRun executes ci/run.sh with a stub docker first on PATH and the given
// extra environment, and returns the recorded `docker run` argv.
func runShRun(t *testing.T, env []string, command ...string) []string {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(dockerStub), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	log := filepath.Join(dir, "run.log")
	cmd := exec.Command("sh", filepath.Join(root, "ci", "run.sh"))
	cmd.Args = append(cmd.Args, command...)
	cmd.Dir = root
	cmd.Env = append([]string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"STUB_RUN_LOG=" + log,
	}, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ci/run.sh failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("stub docker never saw a `docker run`: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func indexOf(argv []string, want string) int {
	for i, a := range argv {
		if a == want {
			return i
		}
	}
	return -1
}

var assignmentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// noSocket points ci/run.sh at a docker socket that does not exist, so a test
// of the rest of the argv does not depend on the host having one.
const noSocket = "BOARD_CI_DOCKER_SOCK=/nonexistent/docker.sock"

func TestRunShSiblingArgv(t *testing.T) {
	argv := runShRun(t, []string{
		"RALPHD_HOST_WORKSPACE=/host/path/board",
		"BOARD_PROBE=x",
		noSocket,
		"NOT_BOARD=leak",
		"AWS_SECRET_ACCESS_KEY=leak",
		"PWD=/not/this",
	}, "printenv", "BOARD_PROBE")

	// --user is the test process's own uid:gid.
	wantUser := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	i := indexOf(argv, "--user")
	if i < 0 || i+1 >= len(argv) || argv[i+1] != wantUser {
		t.Errorf("--user: want %q in argv, got %q", wantUser, argv)
	}

	// The bind mount's source is the HOST workspace.
	var bind, volumes []string
	for j, a := range argv {
		if a == "--mount" && j+1 < len(argv) {
			switch {
			case strings.HasPrefix(argv[j+1], "type=bind,"):
				bind = append(bind, argv[j+1])
			case strings.HasPrefix(argv[j+1], "type=volume,"):
				volumes = append(volumes, argv[j+1])
			}
		}
	}
	if len(bind) != 1 || !strings.Contains(","+bind[0]+",", ",src=/host/path/board,") || !strings.Contains(bind[0], "dst=/w") {
		t.Errorf("bind mount: want src=$RALPHD_HOST_WORKSPACE dst=/w, got %q", bind)
	}

	// A named cache volume (not an anonymous or host path) carries the Go cache.
	goCache := false
	for _, v := range volumes {
		if strings.Contains(","+v+",", ",src=board-go-cache,") && strings.Contains(v, "dst=/go-cache") {
			goCache = true
		}
	}
	if !goCache {
		t.Errorf("want a named volume board-go-cache at /go-cache, got %q", volumes)
	}

	// The command after the image: env carries BOARD_PROBE=x and only BOARD_ names.
	img := indexOf(argv, "board-ci:local")
	if img < 0 {
		t.Fatalf("image board-ci:local missing from argv %q", argv)
	}
	tail := argv[img+1:]
	if indexOf(tail, "BOARD_PROBE=x") < 0 {
		t.Errorf("BOARD_PROBE=x not forwarded: %q", tail)
	}
	for _, a := range tail {
		if assignmentRE.MatchString(a) && !strings.HasPrefix(a, "BOARD_") {
			t.Errorf("non-BOARD_ variable forwarded: %q", a)
		}
	}
	for _, a := range argv {
		if a == "-e" || a == "--env" || a == "--env-file" || strings.HasPrefix(a, "--env=") || strings.Contains(a, "leak") {
			t.Errorf("caller variable reaches docker run argv: %q", a)
		}
	}
	if len(tail) < 2 || tail[len(tail)-2] != "printenv" || tail[len(tail)-1] != "BOARD_PROBE" {
		t.Errorf("the caller's command must come last, got %q", tail)
	}
}

func TestRunShWithoutBoardVariablesAddsNoEnv(t *testing.T) {
	argv := runShRun(t, []string{"RALPHD_HOST_WORKSPACE=/host/ws", noSocket}, "true")
	img := indexOf(argv, "board-ci:local")
	if img < 0 || strings.Join(argv[img+1:], " ") != "env "+noSocket+" true" {
		t.Errorf("want only the caller's BOARD_ variable and command after the image, got %q", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "docker.sock") && !strings.HasPrefix(a, "BOARD_CI_DOCKER_SOCK=") || a == "--group-add" || a == "--add-host" {
			t.Errorf("no docker socket exists, yet argv wires one up: %q", argv)
		}
	}
}

// listenOnSocket creates a real unix socket, which is what ci/run.sh tests for.
func listenOnSocket(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return path
}

// argAfter returns the value following the first occurrence of flag.
func argAfter(argv []string, flag, prefix string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) && strings.HasPrefix(argv[i+1], prefix) {
			return argv[i+1]
		}
	}
	return ""
}

// TestRunShMountsTheDockerSocketForTheSiblingMatrix: the Postgres leg starts its
// container through the docker socket, as a labelled sibling of the sibling, and
// reaches it on the docker host.
func TestRunShMountsTheDockerSocketForTheSiblingMatrix(t *testing.T) {
	sock := listenOnSocket(t)
	info, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	argv := runShRun(t, []string{
		"RALPHD_HOST_WORKSPACE=/host/ws",
		"RALPHD_RUN_ID=run-42",
		"BOARD_CI_DOCKER_SOCK=" + sock,
	}, "true")

	if got := argAfter(argv, "--mount", "type=bind,src="+sock+","); got != "type=bind,src="+sock+",dst=/var/run/docker.sock" {
		t.Errorf("the socket is not bind-mounted at /var/run/docker.sock: %q", argv)
	}
	if gid := fmt.Sprint(info.Sys().(*syscall.Stat_t).Gid); argAfter(argv, "--group-add", gid) != gid {
		t.Errorf("the sibling does not join the socket's group %s: %q", gid, argv)
	}
	if argAfter(argv, "--add-host", "host.docker.internal:") != "host.docker.internal:host-gateway" {
		t.Errorf("host.docker.internal does not resolve to the docker host: %q", argv)
	}
	if argAfter(argv, "--label", "ralphd.role=") != "ralphd.role=sibling" || argAfter(argv, "--label", "ralphd.run=") != "ralphd.run=run-42" {
		t.Errorf("the sibling is not labelled with the run: %q", argv)
	}
	img := indexOf(argv, "board-ci:local")
	tail := argv[img+1:]
	for _, want := range []string{"BOARD_CI_DOCKER_HOST=host.docker.internal", "BOARD_CI_RUN_ID=run-42", "BOARD_CI_DOCKER_SOCK=" + sock} {
		if indexOf(tail, want) < 0 {
			t.Errorf("%s does not reach the command: %q", want, tail)
		}
	}
	if tail[len(tail)-1] != "true" {
		t.Errorf("the caller's command must come last: %q", tail)
	}
}

// TestRunShMemoryCap: BOARD_CI_MEMORY puts a hard limit on the sibling's
// cgroup with swap included (--memory-swap equal to --memory, so a runaway is
// OOM-killed in its container instead of swapping), and without it the
// sibling has no limit.
func TestRunShMemoryCap(t *testing.T) {
	argv := runShRun(t, []string{"RALPHD_HOST_WORKSPACE=/host/ws", noSocket, "BOARD_CI_MEMORY=12g"}, "true")
	img := indexOf(argv, "board-ci:local")
	if img < 0 {
		t.Fatalf("image board-ci:local missing from argv %q", argv)
	}
	for _, want := range []string{"--memory=12g", "--memory-swap=12g"} {
		if i := indexOf(argv, want); i < 0 || i > img {
			t.Errorf("want %s before the image, got %q", want, argv)
		}
	}
	argv = runShRun(t, []string{"RALPHD_HOST_WORKSPACE=/host/ws", noSocket}, "true")
	for _, a := range argv {
		if strings.HasPrefix(a, "--memory") {
			t.Errorf("no BOARD_CI_MEMORY, yet argv caps memory: %q", argv)
		}
	}
}
