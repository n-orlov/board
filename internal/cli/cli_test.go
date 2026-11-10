package cli

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // the driver the store registers, for the schema-version fixture

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/version"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, func(string) string { return "" })
	return code, out.String(), errb.String()
}

func TestVersionPrintsCurrentVersion(t *testing.T) {
	code, out, errs := run("version")
	if code != ExitOK || errs != "" || out != version.Current()+"\n" {
		t.Fatalf("got code=%d out=%q err=%q", code, out, errs)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"version", "extra"}, {"serve", "extra"}} {
		code, out, errs := run(args...)
		if code != 2 || out != "" || errs != Usage+"\n" || strings.Count(errs, "\n") != 1 {
			t.Fatalf("%v: got code=%d out=%q err=%q", args, code, out, errs)
		}
	}
}

func TestServeRejectsUnknownMigrateMode(t *testing.T) {
	var out, errb bytes.Buffer
	env := func(k string) string {
		if k == "BOARD_MIGRATE" {
			return "sometimes"
		}
		return ""
	}
	if code := Run([]string{"serve"}, &out, &errb, env); code != ExitError || !strings.Contains(errb.String(), "BOARD_MIGRATE") {
		t.Fatalf("got code=%d err=%q", code, errb.String())
	}
}

func envWith(kv map[string]string) Env { return func(k string) string { return kv[k] } }

func TestAdminUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"admin"}, {"admin", "bogus"}, {"admin", "migrate", "extra"}} {
		code, out, errs := run(args...)
		if code != ExitUsage || out != "" || errs != Usage+"\n" {
			t.Fatalf("%v: got code=%d out=%q err=%q", args, code, out, errs)
		}
	}
}

func TestAdminMigrateAppliesAndReportsTheVersion(t *testing.T) {
	env := envWith(map[string]string{"BOARD_DB_URL": "sqlite://" + filepath.Join(t.TempDir(), "b.db")})
	for i := 0; i < 2; i++ { // the second run is a no-op
		var out, errb bytes.Buffer
		if code := Run([]string{"admin", "migrate"}, &out, &errb, env); code != ExitOK ||
			out.String() != "ok migrated, schema version 7\n" || errb.Len() != 0 {
			t.Fatalf("run %d: code=%d out=%q err=%q", i, code, out.String(), errb.String())
		}
	}
}

func TestAdminMigrateFailsOnAnUnsupportedURL(t *testing.T) {
	var out, errb bytes.Buffer
	env := envWith(map[string]string{"BOARD_DB_URL": "mysql://x/y"})
	if code := Run([]string{"admin", "migrate"}, &out, &errb, env); code != ExitError ||
		out.Len() != 0 || strings.Count(errb.String(), "\n") != 1 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

func sqliteURL(t *testing.T) string {
	t.Helper()
	return "sqlite://" + filepath.Join(t.TempDir(), "b.db")
}

// serveFor runs serveUntil against url and addr, cancelling it after d.
func serveFor(t *testing.T, url, addr string, migrate bool, d time.Duration) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var errb bytes.Buffer
	code := serveUntil(ctx, &errb, func(string) string { return "" }, url, addr, migrate)
	return code, errb.String()
}

func TestServeUntilServesAndStopsCleanlyWhenTheContextEnds(t *testing.T) {
	for _, migrate := range []bool{true, false} {
		if code, errs := serveFor(t, sqliteURL(t), "127.0.0.1:0", migrate, 300*time.Millisecond); code != ExitOK || errs != "" {
			t.Errorf("migrate=%v: code=%d err=%q", migrate, code, errs)
		}
	}
}

func TestServeUntilFailsWithOneLineOnABadDatabaseOrAddress(t *testing.T) {
	for name, tc := range map[string]struct{ url, addr string }{
		"unsupported database": {"mysql://x/y", "127.0.0.1:0"},
		"unusable address":     {sqliteURL(t), "not-an-address"},
	} {
		code, errs := serveFor(t, tc.url, tc.addr, true, time.Second)
		if code != ExitError || !strings.HasPrefix(errs, "board: ") || strings.Count(errs, "\n") != 1 {
			t.Errorf("%s: code=%d err=%q", name, code, errs)
		}
	}
}

func TestServeUntilRefusesADatabaseNewerThanTheBinary(t *testing.T) {
	url := sqliteURL(t)
	if code := Run([]string{"admin", "migrate"}, io.Discard, io.Discard, envWith(map[string]string{"BOARD_DB_URL": url})); code != ExitOK {
		t.Fatalf("migrate exited %d", code)
	}
	db, err := sql.Open("sqlite", strings.TrimPrefix(url, "sqlite://"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), "INSERT INTO goose_db_version (version_id, is_applied) VALUES (9999, 1)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	for _, migrate := range []bool{true, false} {
		code, errs := serveFor(t, url, "127.0.0.1:0", migrate, time.Second)
		if code != ExitError || !strings.Contains(errs, "9999") {
			t.Errorf("migrate=%v: code=%d err=%q, want a refusal naming version 9999", migrate, code, errs)
		}
	}
}

func TestServeReportsAListenFailureFromTheEnvironment(t *testing.T) {
	var out, errb bytes.Buffer
	env := envWith(map[string]string{"BOARD_DB_URL": sqliteURL(t), "BOARD_LISTEN": "not-an-address"})
	if code := Run([]string{"serve"}, &out, &errb, env); code != ExitError || !strings.Contains(errb.String(), "listen") {
		t.Fatalf("code=%d err=%q", code, errb.String())
	}
}

func TestServeRejectsAMalformedSweepInterval(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"serve"}, &out, &errb, envWith(map[string]string{"BOARD_SWEEP_INTERVAL": "soon"})); code != ExitError ||
		!strings.Contains(errb.String(), "BOARD_SWEEP_INTERVAL") {
		t.Fatalf("got code=%d err=%q", code, errb.String())
	}
}

func TestServeRejectsAnUnreadableStreamPoll(t *testing.T) {
	for _, v := range []string{"soon", "0", "0s", "-2s", "off"} {
		var out, errb bytes.Buffer
		if code := Run([]string{"serve"}, &out, &errb, envWith(map[string]string{"BOARD_STREAM_POLL": v})); code != ExitError ||
			!strings.Contains(errb.String(), "BOARD_STREAM_POLL") || strings.Count(errb.String(), "\n") != 1 {
			t.Errorf("BOARD_STREAM_POLL=%q: code=%d err=%q", v, code, errb.String())
		}
	}
}

// sessionsLeft counts the rows of the sessions table.
func sessionsLeft(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.Reader().QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSweeperDeletesExpiredSessionsAtTheConfiguredIntervalAndNotWhenOff(t *testing.T) {
	for _, tc := range []struct {
		interval string
		want     int
	}{{"5ms", 0}, {"off", 1}} {
		st, err := store.Open(sqliteURL(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		sessions := auth.Sessions{Store: st, TTL: time.Minute, Now: func() time.Time { return old }}
		if _, _, err := sessions.Create(context.Background(), "a@example.com"); err != nil {
			t.Fatal(err)
		}
		var errb bytes.Buffer
		stop := startSweeper(context.Background(), &errb, envWith(map[string]string{"BOARD_SWEEP_INTERVAL": tc.interval}), st)
		deadline := time.Now().Add(3 * time.Second)
		for sessionsLeft(t, st) != tc.want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if tc.want == 1 {
			time.Sleep(50 * time.Millisecond)
		}
		stop()
		if got := sessionsLeft(t, st); got != tc.want || errb.Len() != 0 {
			t.Errorf("interval %s: %d sessions left, want %d (stderr %q)", tc.interval, got, tc.want, errb.String())
		}
		_ = st.Close()
	}
}

func TestSweeperReportsAFailingJobOnStderr(t *testing.T) {
	st, err := store.Open(sqliteURL(t))
	if err != nil {
		t.Fatal(err)
	}
	var errb syncBuffer
	_ = st.Close()
	stop := startSweeper(context.Background(), &errb, envWith(map[string]string{"BOARD_SWEEP_INTERVAL": "5ms"}), st)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(errb.String(), "board: sweep:") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	if !strings.Contains(errb.String(), "board: sweep:") {
		t.Fatalf("stderr %q", errb.String())
	}
}

func TestSweeperWithAMalformedIntervalRunsNothing(_ *testing.T) {
	stop := startSweeper(context.Background(), io.Discard, envWith(map[string]string{"BOARD_SWEEP_INTERVAL": "soon"}), nil)
	stop()
}

// syncBuffer is a bytes.Buffer the sweeper goroutine and the test may share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
