package features

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
)

const usageLine = "usage: board <serve|version>"

// dbLeg returns the database leg named by BOARD_DB_LEG.
func dbLeg() (string, error) {
	switch leg := strings.TrimSpace(os.Getenv("BOARD_DB_LEG")); leg {
	case "", "sqlite":
		return "sqlite", nil
	case "postgres":
		return "", errors.New("BOARD_DB_LEG=postgres: the Postgres leg is not available yet (it arrives with the store)")
	default:
		return "", fmt.Errorf("BOARD_DB_LEG=%q: want sqlite or postgres", leg)
	}
}

// buildBoard compiles the real binary once for the whole run.
func buildBoard(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "board")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/n-orlov/board/cmd/board")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the board binary: %v\n%s", err, out)
	}
	return bin
}

// harness is one scenario's state.
type harness struct {
	binary string
	leg    string

	exit   int
	stdout string
	stderr string

	server *exec.Cmd
	addr   string
	dir    string
}

func (h *harness) env(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "BOARD_") {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

func (h *harness) run(ctx context.Context, line string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "board" {
		return fmt.Errorf("command %q must start with board", line)
	}
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, h.binary, fields[1:]...)
	cmd.Env = h.env()
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	h.stdout, h.stderr = out.String(), errb.String()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		h.exit = 0
	case errors.As(err, &exitErr):
		h.exit = exitErr.ExitCode()
	default:
		return err
	}
	return nil
}

// startServer starts `board serve` on a free port and waits until it accepts.
func (h *harness) startServer(ctx context.Context) (context.Context, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ctx, err
	}
	h.addr = "127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	h.dir, err = os.MkdirTemp("", "board-godog-")
	if err != nil {
		return ctx, err
	}
	h.server = exec.Command(h.binary, "serve")
	h.server.Env = h.env("BOARD_LISTEN="+h.addr, "BOARD_DB_URL=sqlite://"+filepath.Join(h.dir, "board.db"))
	var logs bytes.Buffer
	h.server.Stdout, h.server.Stderr = &logs, &logs
	if err := h.server.Start(); err != nil {
		return ctx, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", h.addr); err == nil {
			conn.Close()
			return ctx, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return ctx, fmt.Errorf("server never listened on %s: %s", h.addr, logs.String())
}

func (h *harness) stopServer() {
	if h.server != nil && h.server.Process != nil {
		_ = h.server.Process.Kill()
		_, _ = h.server.Process.Wait()
	}
	if h.dir != "" {
		_ = os.RemoveAll(h.dir)
	}
}

var versionPattern = regexp.MustCompile(`^(v[0-9][^\s]*|devel(\+[0-9a-f]+)?)(-dirty)?$`)

func registerSteps(sc *godog.ScenarioContext, h *harness) {
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		h.stopServer()
		return ctx, nil
	})
	sc.Step(`^I run "([^"]*)"$`, h.run)
	sc.Step(`^the exit code is (\d+)$`, func(want int) error {
		if h.exit != want {
			return fmt.Errorf("exit code %d, want %d (stderr %q)", h.exit, want, h.stderr)
		}
		return nil
	})
	sc.Step(`^stdout is empty$`, func() error {
		if h.stdout != "" {
			return fmt.Errorf("stdout %q, want empty", h.stdout)
		}
		return nil
	})
	sc.Step(`^stderr is empty$`, func() error {
		if h.stderr != "" {
			return fmt.Errorf("stderr %q, want empty", h.stderr)
		}
		return nil
	})
	sc.Step(`^stdout is a single line holding a version$`, func() error {
		line := strings.TrimSuffix(h.stdout, "\n")
		if strings.Contains(line, "\n") || !versionPattern.MatchString(line) {
			return fmt.Errorf("stdout %q is not a single version line", h.stdout)
		}
		return nil
	})
	sc.Step(`^stderr is exactly the usage line$`, func() error {
		if h.stderr != usageLine+"\n" {
			return fmt.Errorf("stderr %q, want exactly %q", h.stderr, usageLine+"\n")
		}
		return nil
	})
	sc.Step(`^the board server is running on a free port$`, h.startServer)
	sc.Step(`^the server accepts connections on its listen address$`, func() error {
		conn, err := net.DialTimeout("tcp", h.addr, 2*time.Second)
		if err != nil {
			return err
		}
		return conn.Close()
	})
}
