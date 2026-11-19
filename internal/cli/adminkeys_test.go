package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/store"
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
		if code != ExitUsage || out != "" || strings.Count(errs, "\n") != 1 || !strings.HasPrefix(errs, "vh: ") {
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
		if code != ExitError || out != "" || strings.Count(errs, "\n") != 1 || !strings.HasPrefix(errs, "vh: ") {
			t.Errorf("%s: code=%d out=%q err=%q", name, code, out, errs)
		}
	}
	if _, _, errs := runKeys(t, sqliteURL(t), "list"); !strings.Contains(errs, "vh admin migrate") {
		t.Errorf("unmigrated stderr %q does not name the next action", errs)
	}
}

// runKeysEnv is runKeys with extra environment variables, for commands whose
// stdout the test does not read.
func runKeysEnv(t *testing.T, url string, extra map[string]string, args ...string) (int, string) {
	t.Helper()
	env := map[string]string{"BOARD_DB_URL": url}
	for k, v := range extra {
		env[k] = v
	}
	var errb bytes.Buffer
	code := Run(append([]string{"admin", "keys"}, args...), &bytes.Buffer{}, &errb, envWith(env))
	return code, errb.String()
}

// seedActor inserts an actors row of the kind.
func seedActor(t *testing.T, url, id, handle, kind string) {
	t.Helper()
	st, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	d := st.Dialect()
	if _, err := st.Writer().Exec(d.Rebind(d.Insert("actors", []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"})), id, handle, kind, 1, 1); err != nil {
		t.Fatal(err)
	}
}

// keyOwner is the owner handle the list of the store gives the key, or "".
func keyOwner(t *testing.T, url, name string) string {
	t.Helper()
	st, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	k, err := (auth.Keys{Store: st}).Get(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return k.Owner
}

func TestAdminKeysCreateOwnerNamesTheOperatorWhoOwnsTheKey(t *testing.T) {
	url := migrated(t)
	seedActor(t, url, "OP1", "alice-op", "operator")
	seedActor(t, url, "OP2", "bob-op", "operator")
	for _, args := range [][]string{{"create", "a-key", "--owner", "alice-op"}, {"create", "--owner=bob-op", "b-key"}} {
		if code, out, errs := runKeys(t, url, args...); code != ExitOK || errs != "" || !strings.HasPrefix(out, "ok key ") {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
	}
	if got := keyOwner(t, url, "a-key"); got != "alice-op" {
		t.Errorf("a-key owner = %q, want alice-op", got)
	}
	if got := keyOwner(t, url, "b-key"); got != "bob-op" {
		t.Errorf("b-key owner = %q, want bob-op", got)
	}
}

func TestAdminKeysCreateWithoutOwnerDefaultsToVHOperatorHandle(t *testing.T) {
	url := migrated(t)
	seedActor(t, url, "OP1", "operator", "operator")
	seedActor(t, url, "OP2", "boss", "operator")
	if code, _, errs := runKeys(t, url, "create", "plain"); code != ExitOK || errs != "" {
		t.Fatalf("create with the built-in default: code=%d err=%q", code, errs)
	}
	if got := keyOwner(t, url, "plain"); got != "operator" {
		t.Errorf("owner with VH_OPERATOR_HANDLE unset = %q, want operator", got)
	}
	if code, errs := runKeysEnv(t, url, map[string]string{"VH_OPERATOR_HANDLE": "boss"}, "create", "bossed"); code != ExitOK || errs != "" {
		t.Fatalf("create with VH_OPERATOR_HANDLE=boss: code=%d err=%q", code, errs)
	}
	if got := keyOwner(t, url, "bossed"); got != "boss" {
		t.Errorf("owner with VH_OPERATOR_HANDLE=boss = %q, want boss", got)
	}
	// An explicit --owner beats the variable.
	if code, _ := runKeysEnv(t, url, map[string]string{"VH_OPERATOR_HANDLE": "boss"}, "create", "explicit", "--owner", "operator"); code != ExitOK {
		t.Fatalf("create with --owner and VH_OPERATOR_HANDLE: code=%d", code)
	}
	if got := keyOwner(t, url, "explicit"); got != "operator" {
		t.Errorf("owner with --owner operator and VH_OPERATOR_HANDLE=boss = %q, want operator", got)
	}
}

func TestAdminKeysCreateWithoutAnOperatorLeavesTheOwnerNull(t *testing.T) {
	url := migrated(t)
	if code, _, errs := runKeys(t, url, "create", "first"); code != ExitOK || errs != "" {
		t.Fatalf("create on a database with no operator row: code=%d err=%q", code, errs)
	}
	if got := keyOwner(t, url, "first"); got != "" {
		t.Errorf("owner with no operator row = %q, want none", got)
	}
}

func TestAdminKeysCreateRefusesAnOwnerThatIsNoOperatorAndChangesNothing(t *testing.T) {
	url := migrated(t)
	seedActor(t, url, "OP1", "operator", "operator")
	seedActor(t, url, "AG1", "claude@laptop", "agent")
	for _, owner := range []string{"nobody", "claude@laptop"} {
		code, out, errs := runKeys(t, url, "create", "refused", "--owner", owner)
		if code != ExitUsage || out != "" || strings.Count(errs, "\n") != 1 || !strings.Contains(errs, "--owner") || !strings.Contains(errs, owner) {
			t.Errorf("--owner %s: code=%d out=%q err=%q", owner, code, out, errs)
		}
		if _, list, _ := runKeys(t, url, "list"); list != "" {
			t.Errorf("--owner %s left a key behind: %q", owner, list)
		}
	}
	// The same name is free afterwards, so the refusal wrote nothing.
	if code, _, errs := runKeys(t, url, "create", "refused", "--owner", "operator"); code != ExitOK || errs != "" {
		t.Errorf("create after the refusals: code=%d err=%q", code, errs)
	}
}

func TestAdminKeysCreateRejectsAnOwnerFlagWithoutAValidHandle(t *testing.T) {
	url := migrated(t)
	for _, args := range [][]string{{"create", "a", "--owner"}, {"create", "a", "--owner="}, {"create", "a", "--owner", "two words"}} {
		if code, out, errs := runKeys(t, url, args...); code != ExitUsage || out != "" || errs == "" {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
	}
}
