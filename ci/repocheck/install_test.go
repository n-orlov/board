// install_test.go runs the repository's install.sh (SPEC 8.5) with `sh` against
// an httptest release server, with a fake `uname` first on PATH. Nothing here
// reaches GitHub: the API and download base URLs are the loopback server's.
package repocheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// release is one release the fake server serves.
type release struct {
	tag       string
	immutable string // the JSON text of the "immutable" member, "" omits it
	api       string // when not empty, the whole API answer instead of the generated one
	apiStatus int    // when not 0, the API answers this status with an empty body
	tamper    bool   // checksums.txt names a wrong digest for every asset
}

// releaseServer serves /repos/<repo>/releases/{latest,tags/<tag>} and
// /<repo>/releases/download/<tag>/<file>.
type releaseServer struct {
	*httptest.Server
	mu       sync.Mutex
	repo     string
	latest   string
	releases map[string]release
	requests []string
	auth     []string
}

func newReleaseServer(t *testing.T, repo string, releases ...release) *releaseServer {
	t.Helper()
	s := &releaseServer{repo: repo, releases: map[string]release{}}
	for i, r := range releases {
		s.releases[r.tag] = r
		if i == 0 {
			s.latest = r.tag
		}
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *releaseServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *releaseServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.Path)
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.mu.Unlock()
	apiPrefix := "/repos/" + s.repo + "/releases/"
	dlPrefix := "/" + s.repo + "/releases/download/"
	latestPrefix := "/" + s.repo + "/releases/latest/download/"
	switch {
	case r.URL.Path == apiPrefix+"latest":
		s.answerAPI(w, s.latest)
	case strings.HasPrefix(r.URL.Path, apiPrefix+"tags/"):
		s.answerAPI(w, strings.TrimPrefix(r.URL.Path, apiPrefix+"tags/"))
	case strings.HasPrefix(r.URL.Path, latestPrefix):
		s.answerDownload(w, r, s.releases[s.latest], strings.TrimPrefix(r.URL.Path, latestPrefix))
	case strings.HasPrefix(r.URL.Path, dlPrefix):
		tag, file, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, dlPrefix), "/")
		rel, known := s.releases[tag]
		if !ok || !known {
			http.NotFound(w, r)
			return
		}
		s.answerDownload(w, r, rel, file)
	default:
		http.NotFound(w, r)
	}
}

func (s *releaseServer) answerAPI(w http.ResponseWriter, tag string) {
	rel, ok := s.releases[tag]
	switch {
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	case rel.apiStatus != 0:
		w.WriteHeader(rel.apiStatus)
	case rel.api != "":
		_, _ = w.Write([]byte(rel.api))
	default:
		body := fmt.Sprintf("{\n  \"url\": \"x\",\n  \"tag_name\": %q,\n  \"draft\": false", rel.tag)
		if rel.immutable != "" {
			body += ",\n  \"immutable\": " + rel.immutable
		}
		_, _ = w.Write([]byte(body + ",\n  \"assets\": []\n}\n"))
	}
}

// allPairs are the four assets every release carries.
var allPairs = [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

func (s *releaseServer) answerDownload(w http.ResponseWriter, r *http.Request, rel release, file string) {
	if file == "checksums.txt" {
		var b strings.Builder
		for _, p := range allPairs {
			name := assetName(p[0], p[1])
			sum := sha256.Sum256(assetBytes(rel.tag, p[0], p[1]))
			digest := hex.EncodeToString(sum[:])
			if rel.tamper {
				digest = strings.Repeat("0", 64)
			}
			fmt.Fprintf(&b, "%s  %s\n", digest, name)
		}
		_, _ = w.Write([]byte(b.String()))
		return
	}
	for _, p := range allPairs {
		if file == assetName(p[0], p[1]) {
			_, _ = w.Write(assetBytes(rel.tag, p[0], p[1]))
			return
		}
	}
	http.NotFound(w, r)
}

func assetName(os, arch string) string { return "vh_" + os + "_" + arch + ".tar.gz" }

// vhLabel is what the fake binary of an asset prints, so a test can tell which
// release and platform was installed.
func vhLabel(tag, os, arch string) string { return tag + " " + os + "/" + arch }

func assetBytes(tag, osName, arch string) []byte {
	script := "#!/bin/sh\necho 'vh " + vhLabel(tag, osName, arch) + "'\n"
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, f := range []struct{ name, body string }{{"LICENSE", "x\n"}, {"vh", script}} {
		mode := int64(0o644)
		if f.name == "vh" {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			panic(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			panic(err)
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// fakeCurl records each invocation (one line in $FAKE_CURL_LOG) and hands it to the
// real curl only when every URL it was given has a loopback authority (127.0.0.1,
// localhost or [::1], digits-only port, no userinfo). Any other destination is
// recorded in $FAKE_CURL_BLOCKED and answered like an unresolvable host (exit 6)
// without a connection, so no installer test can reach a production endpoint,
// whichever base URL it varies.
const fakeCurl = `#!/bin/sh
echo "$*" >> "$FAKE_CURL_LOG"
for arg in "$@"; do
  case $arg in
    *://*) ;;
    *) continue ;;
  esac
  rest=${arg#*://}
  auth=${rest%%[/?#]*}
  case $arg in
    http://* | https://*) ;;
    *) auth=- ;;
  esac
  case $auth in
    *[!A-Za-z0-9.:[\]]*) auth=- ;;
  esac
  case $auth in
    '[::1]' | '[::1]':[0-9]* | 127.0.0.1 | 127.0.0.1:[0-9]* | localhost | localhost:[0-9]*) ;;
    *)
      echo "$arg" >> "$FAKE_CURL_BLOCKED"
      echo "curl: (6) test stub: refusing a non-loopback destination" >&2
      exit 6
      ;;
  esac
done
exec "$FAKE_REAL_CURL" "$@"
`

const fakeUname = `#!/bin/sh
case "$1" in
-s) printf '%s\n' "$FAKE_UNAME_S" ;;
-m) printf '%s\n' "$FAKE_UNAME_M" ;;
*) exit 2 ;;
esac
`

type installRun struct {
	stdout, stderr string
	code           int
	dir            string   // the install directory
	curlCalls      int      // how many times install.sh started curl
	blocked        []string // non-loopback URLs curl was asked for (the stub refused each)
}

type installSpec struct {
	sysname, machine string
	server           *releaseServer
	env              []string // extra entries, win over the defaults below
	dir              string   // install directory, a fresh one when empty
	onPath           bool     // put the install directory on PATH
}

// runInstall runs `sh install.sh` in a scrubbed environment: no VH_* or
// BOARD_* variable from the ambient one, a fake uname, and a token variable
// that must never be sent anywhere.
func runInstall(t *testing.T, spec installSpec) installRun {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	if spec.dir == "" {
		spec.dir = filepath.Join(t.TempDir(), "bin")
	}
	fakes := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakes, "uname"), []byte(fakeUname), 0o755); err != nil {
		t.Fatalf("write fake uname: %v", err)
	}
	realCurl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatalf("curl not found: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakes, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatalf("write fake curl: %v", err)
	}
	curlLog := filepath.Join(t.TempDir(), "curl.log")
	blockedLog := filepath.Join(t.TempDir(), "blocked.log")
	pathValue := fakes + string(os.PathListSeparator) + os.Getenv("PATH")
	if spec.onPath {
		pathValue = spec.dir + string(os.PathListSeparator) + pathValue
	}
	env := map[string]string{
		"PATH":              pathValue,
		"HOME":              t.TempDir(),
		"TMPDIR":            t.TempDir(),
		"FAKE_CURL_LOG":     curlLog,
		"FAKE_CURL_BLOCKED": blockedLog,
		"FAKE_REAL_CURL":    realCurl,
		"FAKE_UNAME_S":      spec.sysname,
		"FAKE_UNAME_M":      spec.machine,
		"VH_INSTALL_DIR":    spec.dir,
		"GITHUB_TOKEN":      "test-token-that-must-not-be-sent",
		"GH_TOKEN":          "test-token-that-must-not-be-sent",
	}
	if spec.server != nil {
		env["VH_API_URL"] = spec.server.URL
		env["VH_DOWNLOAD_URL"] = spec.server.URL
	}
	for _, kv := range spec.env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", filepath.Join(root, "install.sh"))
	cmd.Dir = t.TempDir()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run install.sh: %v", err)
		}
		code = ee.ExitCode()
	}
	calls := 0
	if data, err := os.ReadFile(curlLog); err == nil {
		calls = strings.Count(string(data), "\n")
	}
	var blocked []string
	if data, err := os.ReadFile(blockedLog); err == nil {
		blocked = strings.Fields(string(data))
	}
	if spec.server != nil && len(blocked) != 0 {
		t.Fatalf("a run against the local release server asked curl for an external URL: %q", blocked)
	}
	return installRun{stdout: stdout.String(), stderr: stderr.String(), code: code, dir: spec.dir, curlCalls: calls, blocked: blocked}
}

func (r installRun) installed(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "vh"))
	if err != nil {
		t.Fatalf("no vh in %s: %v\nstdout:\n%s\nstderr:\n%s", r.dir, err, r.stdout, r.stderr)
	}
	return string(data)
}

// nothingInstalled fails unless the install directory holds no file at all
// (not the binary, not a leftover temporary file).
func (r installRun) nothingInstalled(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(r.dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read %s: %v", r.dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("install directory holds %v, want nothing\nstderr:\n%s", names, r.stderr)
	}
}

func (r installRun) refused(t *testing.T, wantInStderr string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("install.sh exited 0, want a refusal\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, wantInStderr) {
		t.Fatalf("stderr does not contain %q:\n%s", wantInStderr, r.stderr)
	}
}

func (r installRun) succeeded(t *testing.T) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("install.sh exited %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
}

func goodRelease(tag string) release { return release{tag: tag, immutable: "true"} }

func TestInstallShInstallsTheAssetOfEachSupportedPlatform(t *testing.T) {
	cases := []struct {
		sysname, machine, os, arch string
	}{
		{"Linux", "x86_64", "linux", "amd64"},
		{"Linux", "aarch64", "linux", "arm64"},
		{"Darwin", "x86_64", "darwin", "amd64"},
		{"Darwin", "arm64", "darwin", "arm64"},
	}
	for _, c := range cases {
		t.Run(c.os+"_"+c.arch, func(t *testing.T) {
			srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
			run := runInstall(t, installSpec{sysname: c.sysname, machine: c.machine, server: srv})
			run.succeeded(t)
			if got, want := run.installed(t), "#!/bin/sh\necho 'vh "+vhLabel("v1.4.0", c.os, c.arch)+"'\n"; got != want {
				t.Fatalf("installed binary %q, want the %s asset %q", got, assetName(c.os, c.arch), want)
			}
			if want := "vh " + vhLabel("v1.4.0", c.os, c.arch); !strings.Contains(run.stdout, want) {
				t.Fatalf("stdout lacks the `vh version` output %q:\n%s", want, run.stdout)
			}
			info, err := os.Stat(filepath.Join(run.dir, "vh"))
			if err != nil || info.Mode().Perm() != 0o755 {
				t.Fatalf("installed mode %v (err %v), want 0755", info.Mode().Perm(), err)
			}
			entries, _ := os.ReadDir(run.dir)
			if len(entries) != 1 {
				t.Fatalf("install directory holds %d entries after the install, want only vh", len(entries))
			}
			wantReq := []string{
				"/repos/n-orlov/board/releases/latest",
				"/n-orlov/board/releases/download/v1.4.0/" + assetName(c.os, c.arch),
				"/n-orlov/board/releases/download/v1.4.0/checksums.txt",
			}
			if got := srv.seen(); strings.Join(got, "\n") != strings.Join(wantReq, "\n") {
				t.Fatalf("requests %v, want %v (the default repository is n-orlov/board)", got, wantReq)
			}
		})
	}
}

func TestInstallShRefusesAnUnsupportedPlatformAndInstallsNothing(t *testing.T) {
	cases := []struct{ sysname, machine, want string }{
		{"FreeBSD", "amd64", "unsupported OS 'freebsd'"},
		{"Linux", "riscv64", "unsupported architecture 'riscv64'"},
		{"Linux", "i686", "unsupported architecture 'i686'"},
	}
	for _, c := range cases {
		t.Run(c.sysname+"_"+c.machine, func(t *testing.T) {
			srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
			run := runInstall(t, installSpec{sysname: c.sysname, machine: c.machine, server: srv})
			run.refused(t, c.want)
			run.nothingInstalled(t)
			if got := srv.seen(); len(got) != 0 {
				t.Fatalf("an unsupported platform made requests %v", got)
			}
		})
	}
}

func TestInstallShRefusesAChecksumMismatchAndInstallsNothing(t *testing.T) {
	rel := goodRelease("v1.4.0")
	rel.tamper = true
	srv := newReleaseServer(t, "n-orlov/board", rel)
	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv})
	run.refused(t, "checksum mismatch")
	run.nothingInstalled(t)
}

func TestInstallShKeepsTheOldBinaryOnARefusal(t *testing.T) {
	rel := goodRelease("v1.4.0")
	rel.tamper = true
	srv := newReleaseServer(t, "n-orlov/board", rel)
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "vh")
	if err := os.WriteFile(old, []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, dir: dir})
	run.refused(t, "checksum mismatch")
	if got := run.installed(t); got != "old\n" {
		t.Fatalf("the old binary became %q after a refusal", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("a refusal left %d entries in the install directory, want only the old vh", len(entries))
	}
}

// notConfirmed are the release API answers that do not confirm immutability.
var notConfirmed = []struct {
	name string
	rel  release
	want string
}{
	{"false", release{tag: "v1.4.0", immutable: "false"}, "is not an immutable release"},
	{"absent", release{tag: "v1.4.0"}, "could not be confirmed immutable"},
	{"string true", release{tag: "v1.4.0", immutable: `"true"`}, "could not be confirmed immutable"},
	{"null", release{tag: "v1.4.0", immutable: "null"}, "could not be confirmed immutable"},
	{"api error", release{tag: "v1.4.0", apiStatus: 500}, "could not be confirmed immutable"},
	{"nested decoy", release{tag: "v1.4.0", api: `{"assets":[{"immutable":true}],"tag_name":"v1.4.0","immutable":false}`}, "is not an immutable release"},
}

func TestInstallShRefusesAReleaseThatIsNotConfirmedImmutable(t *testing.T) {
	for _, c := range notConfirmed {
		t.Run(c.name, func(t *testing.T) {
			srv := newReleaseServer(t, "n-orlov/board", c.rel)
			run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv})
			run.refused(t, c.want)
			run.nothingInstalled(t)
			for _, p := range srv.seen() {
				if strings.Contains(p, "/download/") {
					t.Fatalf("a refused release still downloaded %s", p)
				}
			}
		})
	}
}

func TestInstallShOptOutInstallsAReleaseThatIsNotConfirmedImmutable(t *testing.T) {
	for _, c := range notConfirmed {
		t.Run(c.name, func(t *testing.T) {
			srv := newReleaseServer(t, "n-orlov/board", c.rel)
			run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{"VH_REQUIRE_IMMUTABLE=0"}})
			run.succeeded(t)
			if !strings.Contains(run.installed(t), "v1.4.0 linux/amd64") {
				t.Fatalf("installed %q, want the v1.4.0 linux/amd64 binary", run.installed(t))
			}
			if !strings.Contains(run.stderr, "VH_REQUIRE_IMMUTABLE=0") {
				t.Fatalf("the opt-out printed no note:\n%s", run.stderr)
			}
		})
	}
}

func TestInstallShOptOutFallsBackToTheLatestDownloadWhenTheAPIIsDown(t *testing.T) {
	// A server with no API answer for "latest" at all: the opt-out still installs
	// from the latest-download path.
	inner := newReleaseServer(t, "n-orlov/board", goodRelease("v2.0.0"))
	mux := http.NewServeMux()
	mux.HandleFunc("/n-orlov/board/releases/latest/download/", func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/n-orlov/board/releases/latest/download/")
		inner.answerDownload(w, r, inner.releases["v2.0.0"], file)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	run := runInstall(t, installSpec{sysname: "Darwin", machine: "arm64", env: []string{
		"VH_REQUIRE_IMMUTABLE=0", "VH_API_URL=" + srv.URL, "VH_DOWNLOAD_URL=" + srv.URL,
	}})
	run.succeeded(t)
	if !strings.Contains(run.installed(t), "v2.0.0 darwin/arm64") {
		t.Fatalf("installed %q", run.installed(t))
	}
}

func TestInstallShEnforcesImmutabilityForEveryValueButZero(t *testing.T) {
	for _, value := range []string{"1", "no", "false", "00", "", " 0", "0 "} {
		t.Run("value="+value, func(t *testing.T) {
			srv := newReleaseServer(t, "n-orlov/board", release{tag: "v1.4.0", immutable: "false"})
			run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{"VH_REQUIRE_IMMUTABLE=" + value}})
			run.refused(t, "is not an immutable release")
			run.nothingInstalled(t)
		})
	}
}

func TestInstallShPinsTheVersionOfVHVersion(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v9.9.9"), goodRelease("v1.2.3"))
	run := runInstall(t, installSpec{sysname: "Linux", machine: "aarch64", server: srv, env: []string{"VH_VERSION=v1.2.3"}})
	run.succeeded(t)
	if !strings.Contains(run.installed(t), "v1.2.3 linux/arm64") {
		t.Fatalf("installed %q, want the pinned v1.2.3", run.installed(t))
	}
	for _, p := range srv.seen() {
		if strings.Contains(p, "v9.9.9") || strings.HasSuffix(p, "/latest") {
			t.Fatalf("a pinned install touched %s", p)
		}
	}
	if got := srv.seen()[0]; got != "/repos/n-orlov/board/releases/tags/v1.2.3" {
		t.Fatalf("first request %s, want the release API of the pinned tag", got)
	}
}

func TestInstallShRefusesAPinnedVersionThatIsUnknownOrMalformed(t *testing.T) {
	for _, version := range []string{"v8.8.8", "1.2.3", "v1.2", "v1.2.3/../../x", "latest2", "v1.2.3 v1.2.4"} {
		t.Run(version, func(t *testing.T) {
			srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.2.3"))
			run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{"VH_VERSION=" + version}})
			if run.code == 0 {
				t.Fatalf("VH_VERSION=%q installed:\n%s", version, run.stdout)
			}
			run.nothingInstalled(t)
		})
	}
}

func TestInstallShReadsTheReleaseOfVHRepo(t *testing.T) {
	srv := newReleaseServer(t, "example/village-hall", goodRelease("v3.0.0"))
	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{"VH_REPO=example/village-hall"}})
	run.succeeded(t)
	if !strings.Contains(run.installed(t), "v3.0.0 linux/amd64") {
		t.Fatalf("installed %q", run.installed(t))
	}
	for _, repo := range []string{"example", "a/b/c", "a b/c", "../x/y", "a/b;c"} {
		bad := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{"VH_REPO=" + repo}})
		bad.refused(t, "is not OWNER/NAME")
		bad.nothingInstalled(t)
	}
}

func TestInstallShReplacesAnExistingBinaryThroughARename(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "vh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	// An open handle on the old file, as a running binary would hold.
	held, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, dir: dir})
	run.succeeded(t)
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatalf("the inode did not change: the binary was overwritten in place instead of renamed")
	}
	if !strings.Contains(run.installed(t), "v1.4.0 linux/amd64") {
		t.Fatalf("installed %q, want the new binary", run.installed(t))
	}
	if after.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v, want 0755", after.Mode().Perm())
	}
	old := make([]byte, 64)
	n, _ := held.Read(old)
	if string(old[:n]) != "#!/bin/sh\necho old\n" {
		t.Fatalf("the held old file changed to %q", old[:n])
	}
}

func TestInstallShWarnsWhenTheDirectoryIsNotOnPath(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
	off := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv})
	off.succeeded(t)
	if want := off.dir + " is not on your PATH"; !strings.Contains(off.stderr, want) {
		t.Fatalf("stderr lacks the PATH warning %q:\n%s", want, off.stderr)
	}
	on := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, onPath: true})
	on.succeeded(t)
	if strings.Contains(on.stderr, "PATH") {
		t.Fatalf("a directory on PATH still got a warning:\n%s", on.stderr)
	}
}

func TestInstallShDefaultsTheDirectoryToLocalBinInHome(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
	home := t.TempDir()
	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{"HOME=" + home, "VH_INSTALL_DIR="}})
	run.succeeded(t)
	if data, err := os.ReadFile(filepath.Join(home, ".local", "bin", "vh")); err != nil || !strings.Contains(string(data), "v1.4.0") {
		t.Fatalf("no vh under $HOME/.local/bin (err %v)\nstderr:\n%s", err, run.stderr)
	}
}

func TestInstallShRefusesANonLoopbackHTTPOverride(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
	for _, variable := range []string{"VH_API_URL", "VH_DOWNLOAD_URL"} {
		for _, url := range []string{
			"http://example.com", "http://example.com:8080/x", "http://127.0.0.1.example.com",
			"http://127.0.0.1@example.com", "http://localhost.example.com", "http://10.0.0.1:80",
			"http://[::2]", "ftp://127.0.0.1", "127.0.0.1:80", "http://",
			// authority confusion: loopback-looking text that is not the authority's host
			"http://127.0.0.1:80@example.com", "http://127.0.0.1:x@example.com/", "http://localhost:@example.com",
			"http://[::1]:80@example.com", "http://user:pw@localhost", "http://localhost:80@127.0.0.1",
			`http://127.0.0.1\@example.com`, `http://localhost\.example.com`, "http://127.0.0.1.:80",
			"http://[::1]x", "http://[::1]:80@[::2]", "http://localhost:80:90", "http://127.0.0.1:", "http://localhost:8a",
			"http://LOCALHOST:80", "http://127.0.0.1 @example.com", "http://[::1", "http://::1", "http://127.1",
			"http://0.0.0.0:80", "http://localhost%2540example.com", "http://[::1]%25x",
			// loopback text after the authority's end (query or fragment) does not make the host loopback
			"http://example.com?@localhost", "http://example.com#127.0.0.1", "http://example.com/?h=[::1]",
			// malformed port: outside 1-65535, however many digits or leading zeros it has
			"http://localhost:65536/path", "http://[::1]:65536/path", "http://127.0.0.1:9999999999999999999999/path",
			"http://localhost:99999", "http://127.0.0.1:0", "http://[::1]:000", "http://localhost:100000/x",
			"http://[::1]:0065536", "http://localhost:4294967376", "http://127.0.0.1:18446744073709551697",
		} {
			t.Run(variable+"="+url, func(t *testing.T) {
				run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv, env: []string{variable + "=" + url}})
				run.refused(t, variable+" '"+url+"' is refused: only https is allowed")
				run.nothingInstalled(t)
				if run.curlCalls != 0 {
					t.Fatalf("a refused override started curl %d time(s), want 0", run.curlCalls)
				}
			})
		}
	}
	if got := srv.seen(); len(got) != 0 {
		t.Fatalf("a refused override still sent requests %v", got)
	}
}

func TestInstallShAcceptsLoopbackHTTPOverrides(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", env: []string{
		"VH_API_URL=http://localhost:" + port + "/", "VH_DOWNLOAD_URL=http://localhost:" + port,
	}})
	run.succeeded(t)
	if !strings.Contains(run.installed(t), "v1.4.0 linux/amd64") {
		t.Fatalf("installed %q", run.installed(t))
	}
	// [::1] passes the check (whether anything listens there is another matter).
	v6 := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", env: []string{
		"VH_API_URL=http://[::1]:1", "VH_DOWNLOAD_URL=http://[::1]:1",
	}})
	if v6.code == 0 || strings.Contains(v6.stderr, "only https is allowed") {
		t.Fatalf("a [::1] override was refused as non-loopback (exit %d):\n%s", v6.code, v6.stderr)
	}
}

func TestInstallShAcceptsEveryLoopbackAuthorityForm(t *testing.T) {
	for _, url := range []string{
		"http://127.0.0.1", "http://127.0.0.1:1", "http://127.0.0.1:1/", "http://127.0.0.1/x",
		"http://localhost", "http://localhost:1", "http://localhost:1/x", "http://[::1]", "http://[::1]:1/x",
		"http://localhost:65535", "http://127.0.0.1:00001/x", "http://[::1]:0001",
		"http://localhost?x=1", "http://127.0.0.1:8080#frag", "http://[::1]/p?q=@example.com",
		"https://example.com", "https://127.0.0.1:1", "https://user@example.com",
	} {
		for _, variable := range []string{"VH_API_URL", "VH_DOWNLOAD_URL"} {
			t.Run(variable+"="+url, func(t *testing.T) {
				// Both bases are set, so the run can only reach the URL under test and a
				// loopback address nothing listens on (port 1): it fails at the request,
				// never at the check, and never leaves loopback (the curl stub refuses
				// any other destination, such as https://example.com).
				other := "VH_DOWNLOAD_URL"
				if variable == other {
					other = "VH_API_URL"
				}
				run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", env: []string{variable + "=" + url, other + "=http://127.0.0.1:1"}})
				if strings.Contains(run.stderr, "is refused") {
					t.Fatalf("%s was refused:\n%s", url, run.stderr)
				}
				if run.curlCalls == 0 {
					t.Fatalf("%s=%s was accepted but curl never started:\n%s", variable, url, run.stderr)
				}
				for _, b := range run.blocked {
					if !strings.HasPrefix(url, "https://") || !strings.HasPrefix(b, "https://") {
						t.Fatalf("a loopback http override reached the non-loopback destination %q", b)
					}
				}
			})
		}
	}
}

// stubProbe runs the fake curl with a sentinel in place of the real curl and reports
// whether the sentinel (a network-capable curl) was started, and what was blocked.
func stubProbe(t *testing.T, args ...string) (delegated bool, blocked []string, code int) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "curl")
	sentinel := filepath.Join(dir, "real-curl")
	for path, body := range map[string]string{stub: fakeCurl, sentinel: "#!/bin/sh\necho started >> \"$SENTINEL_LOG\"\n"} {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sentinelLog, blockedLog := filepath.Join(dir, "sentinel.log"), filepath.Join(dir, "blocked.log")
	cmd := exec.Command("sh", append([]string{stub}, args...)...)
	cmd.Env = []string{
		"FAKE_CURL_LOG=" + filepath.Join(dir, "calls.log"), "FAKE_CURL_BLOCKED=" + blockedLog,
		"FAKE_REAL_CURL=" + sentinel, "SENTINEL_LOG=" + sentinelLog,
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run curl stub: %v", err)
		}
		code = ee.ExitCode()
	}
	_, err := os.Stat(sentinelLog)
	if data, err := os.ReadFile(blockedLog); err == nil {
		blocked = strings.Fields(string(data))
	}
	return err == nil, blocked, code
}

func TestTheCurlStubNeverDelegatesAnExternalURLToARealCurl(t *testing.T) {
	external := []string{
		"https://api.github.com/repos/n-orlov/board/releases/latest", "https://github.com", "https://example.com",
		"http://example.com", "https://127.0.0.1.example.com/x", "http://127.0.0.1@example.com", "http://127.0.0.1:80@example.com",
		"https://localhost@example.com", "http://localhost.example.com:80", "ftp://127.0.0.1/x", "https://[::2]",
		"http://10.0.0.1", "http://127.0.0.1:x", "http://0.0.0.0", `http://127.0.0.1\@example.com`, "gopher://localhost",
	}
	for _, url := range external {
		t.Run(url, func(t *testing.T) {
			delegated, blocked, code := stubProbe(t, "-fsSL", "-o", "x", url)
			if delegated {
				t.Fatalf("the stub delegated %q to the real curl", url)
			}
			if code != 6 || len(blocked) != 1 || blocked[0] != url {
				t.Fatalf("exit %d, blocked %q, want exit 6 and %q recorded", code, blocked, url)
			}
		})
	}
	// An external URL anywhere among several arguments blocks the whole call.
	if delegated, blocked, _ := stubProbe(t, "http://127.0.0.1:1/a", "-o", "x", "https://example.com/b"); delegated || len(blocked) != 1 {
		t.Fatalf("mixed call delegated=%v blocked=%q", delegated, blocked)
	}
	for _, url := range []string{"http://127.0.0.1:1/x", "http://localhost", "https://localhost:8443/p?q=@example.com", "http://[::1]:9/x"} {
		if delegated, blocked, code := stubProbe(t, "-fsSL", url); !delegated || len(blocked) != 0 || code != 0 {
			t.Fatalf("loopback %q: delegated=%v blocked=%q exit %d, want it passed through", url, delegated, blocked, code)
		}
	}
}

func TestEveryInstallerRunWithAnExternalBaseNeverReachesTheNetwork(t *testing.T) {
	// Each external base, on either variable and on both, ends at the stub's refusal
	// with nothing installed and the production destination recorded, not requested.
	for _, base := range []string{"https://api.github.com", "https://github.com", "https://example.com/base"} {
		for _, vars := range [][]string{{"VH_API_URL"}, {"VH_DOWNLOAD_URL"}, {"VH_API_URL", "VH_DOWNLOAD_URL"}} {
			t.Run(base+" via "+strings.Join(vars, "+"), func(t *testing.T) {
				var env []string
				for _, v := range vars {
					env = append(env, v+"="+base)
				}
				run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", env: env})
				if run.code == 0 || len(run.blocked) == 0 {
					t.Fatalf("exit %d, blocked %q: want a failure at the stub's refusal", run.code, run.blocked)
				}
				for _, b := range run.blocked {
					if !strings.HasPrefix(b, "https://") {
						t.Fatalf("blocked %q is not an https URL", b)
					}
				}
				run.nothingInstalled(t)
			})
		}
	}
}

func TestInstallShSendsNoCredential(t *testing.T) {
	srv := newReleaseServer(t, "n-orlov/board", goodRelease("v1.4.0"))
	run := runInstall(t, installSpec{sysname: "Linux", machine: "x86_64", server: srv})
	run.succeeded(t)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.auth) == 0 {
		t.Fatal("the server saw no request")
	}
	for i, a := range srv.auth {
		if a != "" {
			t.Fatalf("request %s carried an Authorization header", srv.requests[i])
		}
	}
}

// curlCallsWithoutHTTPSOnly returns each curl call in a shell script (a line
// continued with a trailing backslash is one call) that lacks `--proto =https`
// or whose --proto value allows anything but https and (for a loopback test
// override) http.
func curlCallsWithoutHTTPSOnly(script string) []string {
	var calls []string
	var current string
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if current != "" {
			current += " " + trimmed
		} else {
			current = trimmed
		}
		if strings.HasSuffix(current, "\\") {
			current = strings.TrimSuffix(current, "\\")
			continue
		}
		if curlWord.MatchString(current) && !protoHTTPS.MatchString(current) {
			calls = append(calls, current)
		}
		current = ""
	}
	return calls
}

var (
	curlWord   = regexp.MustCompile(`(^|[\s;&|(){]|\$\()curl\s`)
	protoHTTPS = regexp.MustCompile(`--proto =https(,http)?\s`)
)

func TestInstallShEveryCurlCallIsHTTPSOnly(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	if !curlWord.MatchString(script) {
		t.Fatal("install.sh has no curl call: the check below would pass on nothing")
	}
	if bad := curlCallsWithoutHTTPSOnly(script); len(bad) != 0 {
		t.Fatalf("curl calls without --proto =https: %q", bad)
	}
	for _, banned := range []string{"Authorization", "GITHUB_TOKEN", "GH_TOKEN", "gh release", "gh auth"} {
		if strings.Contains(script, banned) {
			t.Fatalf("install.sh handles a token (%q): the repository is public and every download is anonymous", banned)
		}
	}
}

func TestTheCurlCheckFailsOnACallWithoutProtoHTTPS(t *testing.T) {
	cases := map[string]string{
		"plain":         "curl -fsSL -o x https://example.com/x\n",
		"substituted":   "body=$(curl -fsSL https://example.com)\n",
		"after pipe":    "echo | curl -fsSL https://example.com\n",
		"wrong proto":   "curl --proto =http -fsSL http://example.com\n",
		"wider proto":   "curl --proto =https,http,ftp -fsSL http://example.com\n",
		"continuation":  "curl -fsSL \\\n  -o x https://example.com/x\n",
		"proto on next": "curl -fsSL \\\n  --proto =http -o x u\n",
	}
	for name, script := range cases {
		if got := curlCallsWithoutHTTPSOnly(script); len(got) != 1 {
			t.Errorf("%s: %q flagged %d calls, want 1", name, script, len(got))
		}
	}
	good := "# curl in a comment\ncurl --proto =https -fsSL x\ncurl -fsSL \\\n  --proto =https,http y\nx=$(curl --proto =https z)\n"
	if got := curlCallsWithoutHTTPSOnly(good); len(got) != 0 {
		t.Errorf("good script flagged %q", got)
	}
}

// section8_5 returns SPEC.md's "### 8.5" section.
func section8_5(t *testing.T) string {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "SPEC.md"))
	if err != nil {
		t.Fatal(err)
	}
	spec := string(data)
	start := strings.Index(spec, "### 8.5 ")
	end := strings.Index(spec, "### 8.6 ")
	if start < 0 || end < start {
		t.Fatal("SPEC.md has no §8.5 section ending at §8.6")
	}
	return spec[start:end]
}

func TestSpecSection85DescribesTheInstallerForThePublicRepository(t *testing.T) {
	section := section8_5(t)
	for _, want := range []string{
		"VH_VERSION", "VH_INSTALL_DIR", "VH_REQUIRE_IMMUTABLE", "VH_REPO", "n-orlov/board",
		"vh_<os>_<arch>.tar.gz", "checksums.txt", "anonymous", "public repository", "VH_API_URL", "VH_DOWNLOAD_URL",
		"--proto =https",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("SPEC §8.5 does not mention %q", want)
		}
	}
	for _, banned := range []string{"while the repository is private", "is private", "board_<os>", "<owner>", "BOARD_REQUIRE_IMMUTABLE"} {
		if strings.Contains(section, banned) {
			t.Errorf("SPEC §8.5 still contains %q", banned)
		}
	}
}
