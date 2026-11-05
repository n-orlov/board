package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/auth"
)

func runKeys(t *testing.T, url string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(append([]string{"admin", "keys"}, args...), &out, &errb, envWith(map[string]string{"BOARD_DB_URL": url}))
	return code, out.String(), errb.String()
}

func migrated(t *testing.T) string {
	t.Helper()
	url := sqliteURL(t)
	if code := Run([]string{"admin", "migrate"}, &bytes.Buffer{}, &bytes.Buffer{}, envWith(map[string]string{"BOARD_DB_URL": url})); code != ExitOK {
		t.Fatalf("migrate exited %d", code)
	}
	return url
}

func TestAdminKeysCreatePrintsTheKeyOnceAndListNeverDoes(t *testing.T) {
	url := migrated(t)
	code, out, errs := runKeys(t, url, "create", "workstation")
	fields := strings.Fields(out)
	if code != ExitOK || errs != "" || len(fields) != 5 || strings.Count(out, "\n") != 1 {
		t.Fatalf("create: code=%d out=%q err=%q", code, out, errs)
	}
	key := fields[4]
	if fields[0] != "ok" || fields[1] != "key" || fields[2] != "workstation" || fields[3] != "created" || !auth.WellFormed(key) {
		t.Fatalf("create output %q", out)
	}
	code, out, errs = runKeys(t, url, "list")
	if code != ExitOK || errs != "" || strings.Contains(out, key) {
		t.Fatalf("list: code=%d out=%q err=%q", code, out, errs)
	}
	want := "workstation brd_" + key[4:12] + " active created " + time.Now().UTC().Format(dateLayout) + "\n"
	if out != want {
		t.Fatalf("list output %q, want %q", out, want)
	}
}

func TestAdminKeysCreateWithExpiryShowsItInCreateAndList(t *testing.T) {
	url := migrated(t)
	code, out, _ := runKeys(t, url, "create", "--expires", "90d", "ralphd")
	exp := time.Now().UTC().Add(90 * 24 * time.Hour).Format(dateLayout)
	if code != ExitOK || !strings.HasPrefix(out, "ok key ralphd created exp "+exp+" brd_") {
		t.Fatalf("create: code=%d out=%q", code, out)
	}
	if code, out, _ = runKeys(t, url, "create", "laptop", "--expires=12h"); code != ExitOK || !strings.Contains(out, " exp ") {
		t.Fatalf("create after the name: code=%d out=%q", code, out)
	}
	if _, out, _ = runKeys(t, url, "list"); !strings.Contains(out, "ralphd") || !strings.Contains(out, " exp "+exp+"\n") {
		t.Fatalf("list output %q", out)
	}
}

func TestAdminKeysCreateRefusesADuplicateWithOneStderrLine(t *testing.T) {
	url := migrated(t)
	runKeys(t, url, "create", "dup")
	code, out, errs := runKeys(t, url, "create", "dup")
	if code != ExitError || out != "" || strings.Count(errs, "\n") != 1 || !strings.Contains(errs, `"dup" already exists`) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestAdminKeysRevokeMarksTheKeyAndIsIdempotent(t *testing.T) {
	url := migrated(t)
	runKeys(t, url, "create", "old")
	if code, out, errs := runKeys(t, url, "revoke", "old"); code != ExitOK || out != "ok key old revoked\n" || errs != "" {
		t.Fatalf("revoke: code=%d out=%q err=%q", code, out, errs)
	}
	if code, out, _ := runKeys(t, url, "revoke", "old"); code != ExitOK || out != "ok key old already revoked\n" {
		t.Fatalf("second revoke: code=%d out=%q", code, out)
	}
	if _, out, _ := runKeys(t, url, "list"); !strings.Contains(out, "old brd_") || !strings.Contains(out, " revoked created ") ||
		!strings.Contains(out, " revoked "+time.Now().UTC().Format(dateLayout)) {
		t.Fatalf("list output %q", out)
	}
	code, out, errs := runKeys(t, url, "revoke", "nobody")
	if code != ExitNotFound || out != "" || strings.Count(errs, "\n") != 1 || !strings.Contains(errs, "not found") {
		t.Fatalf("unknown: code=%d out=%q err=%q", code, out, errs)
	}
}

func TestAdminKeysUsageErrorsPrintTheKeysUsageLine(t *testing.T) {
	url := migrated(t)
	for _, args := range [][]string{
		{}, {"bogus"}, {"create"}, {"create", "a", "b"}, {"create", "a", "--expires"}, {"create", "a", "--bogus"},
		{"list", "extra"}, {"revoke"}, {"revoke", "a", "b"},
	} {
		code, out, errs := runKeys(t, url, args...)
		if code != ExitUsage || out != "" || errs != KeysUsage+"\n" {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
	}
}

func TestAdminKeysRejectsABadNameOrExpiryAsAUsageError(t *testing.T) {
	url := migrated(t)
	for _, args := range [][]string{
		{"create", "bad name"}, {"create", "a", "--expires", "soon"}, {"create", "a", "--expires", "0d"},
		{"create", "a", "--expires", "100000d"}, {"create", "a", "--expires", "36501d"}, {"create", "a", "--expires", "90"},
	} {
		code, out, errs := runKeys(t, url, args...)
		if code != ExitUsage || out != "" || strings.Count(errs, "\n") != 1 || !strings.HasPrefix(errs, "board: ") {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
	}
}

func TestParseExpiry(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"1d": 24 * time.Hour, "90d": 90 * 24 * time.Hour, "12h": 12 * time.Hour, "36500d": 36500 * 24 * time.Hour,
	} {
		if got, err := ParseExpiry(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
}

func TestAdminKeysOnAnUnmigratedOrUnsupportedDatabaseFailsWithOneLine(t *testing.T) {
	for name, url := range map[string]string{"unmigrated": sqliteURL(t), "unsupported": "mysql://x/y"} {
		code, out, errs := runKeys(t, url, "list")
		if code != ExitError || out != "" || strings.Count(errs, "\n") != 1 || !strings.HasPrefix(errs, "board: ") {
			t.Errorf("%s: code=%d out=%q err=%q", name, code, out, errs)
		}
	}
	if _, _, errs := runKeys(t, sqliteURL(t), "list"); !strings.Contains(errs, "board admin migrate") {
		t.Errorf("unmigrated stderr %q does not name the next action", errs)
	}
}
