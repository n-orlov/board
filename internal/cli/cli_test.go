package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

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
