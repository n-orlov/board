package features

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	_ "modernc.org/sqlite" // the driver the schema-version step writes through
)

const usageLine = "usage: board <serve|version|admin>"

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

	status int

	server *exec.Cmd
	addr   string
	dir    string
	dbURL  string
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
	return h.runWith(ctx, line)
}

func (h *harness) runWithVar(ctx context.Context, line, name, value string) error {
	return h.runWith(ctx, line, name+"="+value)
}

func (h *harness) runWith(ctx context.Context, line string, extra ...string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "board" {
		return fmt.Errorf("command %q must start with board", line)
	}
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, h.binary, fields[1:]...)
	if h.dbURL != "" {
		extra = append([]string{"BOARD_DB_URL=" + h.dbURL}, extra...)
	}
	cmd.Env = h.env(extra...)
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
	return h.startServerWith(ctx)
}

// startServerWithVar starts the server with one extra BOARD_* variable set.
func (h *harness) startServerWithVar(ctx context.Context, name, value string) (context.Context, error) {
	return h.startServerWith(ctx, name+"="+value)
}

func (h *harness) startServerWith(ctx context.Context, extra ...string) (context.Context, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ctx, err
	}
	h.addr = "127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	if err := h.freshDatabase(); err != nil {
		return ctx, err
	}
	h.server = exec.Command(h.binary, "serve")
	h.server.Env = h.env(append([]string{"BOARD_LISTEN=" + h.addr, "BOARD_DB_URL=" + h.dbURL}, extra...)...)
	var logs bytes.Buffer
	h.server.Stdout, h.server.Stderr = &logs, &logs
	if err := h.server.Start(); err != nil {
		return ctx, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", h.addr); err == nil {
			_ = conn.Close()
			return ctx, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return ctx, fmt.Errorf("server never listened on %s: %s", h.addr, logs.String())
}

// freshDatabase names a database file in the scenario's temp directory, once.
// A scenario that already prepared one (a migrated or too-new store) keeps it.
func (h *harness) freshDatabase() error {
	if h.dbURL != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "board-godog-")
	if err != nil {
		return err
	}
	h.dir = dir
	h.dbURL = "sqlite://" + filepath.Join(dir, "board.db")
	return nil
}

// recordSchemaVersion makes the scenario's database claim goose version v as
// applied, the way a newer binary's migration would have.
func (h *harness) recordSchemaVersion(ctx context.Context, v int) error {
	if err := h.freshDatabase(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", strings.TrimPrefix(h.dbURL, "sqlite://"))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", v)
	return err
}

// get sends a credential-less GET to the running server and records the status.
func (h *harness) get(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+h.addr+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	h.status = resp.StatusCode
	return nil
}

func (h *harness) stopServer() {
	if h.server != nil && h.server.Process != nil {
		_ = h.server.Process.Kill()
		_, _ = h.server.Process.Wait()
	}
	if h.dir != "" {
		_ = os.RemoveAll(h.dir)
	}
	h.dir, h.dbURL = "", ""
}

var versionPattern = regexp.MustCompile(`^(v[0-9][^\s]*|devel(\+[0-9a-f]+)?)(-dirty)?$`)

func registerSteps(sc *godog.ScenarioContext, h *harness) {
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		h.stopServer()
		return ctx, nil
	})
	sc.Step(`^I run "([^"]*)"$`, h.run)
	sc.Step(`^I run "([^"]*)" with (BOARD_[A-Z_]+)=(\w+)$`, h.runWithVar)
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
	sc.Step(`^a new SQLite database for the board commands$`, h.freshDatabase)
	sc.Step(`^the database records schema version (\d+) as applied$`, h.recordSchemaVersion)
	sc.Step(`^stderr is exactly one line containing "([^"]*)"$`, func(want string) error {
		if strings.Count(h.stderr, "\n") != 1 || !strings.HasSuffix(h.stderr, "\n") || !strings.Contains(h.stderr, want) {
			return fmt.Errorf("stderr %q is not exactly one line containing %q", h.stderr, want)
		}
		return nil
	})
	sc.Step(`^stdout is exactly "([^"]*)"$`, func(want string) error {
		if h.stdout != want+"\n" {
			return fmt.Errorf("stdout %q, want %q", h.stdout, want+"\n")
		}
		return nil
	})
	sc.Step(`^the board server is running on a free port$`, h.startServer)
	sc.Step(`^the board server is running on a free port with (BOARD_[A-Z_]+)=(\w+)$`, h.startServerWithVar)
	sc.Step(`^I request GET "([^"]*)" without credentials$`, h.get)
	sc.Step(`^the response status is (\d+)$`, func(want int) error {
		if h.status != want {
			return fmt.Errorf("response status %d, want %d", h.status, want)
		}
		return nil
	})
	sc.Step(`^the server accepts connections on its listen address$`, func() error {
		conn, err := net.DialTimeout("tcp", h.addr, 2*time.Second)
		if err != nil {
			return err
		}
		return conn.Close()
	})
}
