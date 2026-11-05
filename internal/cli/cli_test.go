package cli

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // the driver the store registers, for the schema-version fixture

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
			out.String() != "ok migrated, schema version 2\n" || errb.Len() != 0 {
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
