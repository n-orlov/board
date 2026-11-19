package envvar

import (
	"slices"
	"testing"
)

// r1Suffixes is the list of the 25 product variables (R1's 24 and R12's OIDC_OPERATORS), in table order.
var r1Suffixes = []string{
	"URL", "API_KEY", "ACTOR", "WAY_BACK", "PUBLIC_URL", "LISTEN", "DB_URL", "MIGRATE",
	"SQLITE_CHECKPOINT", "SWEEP_INTERVAL", "NOTIFY_URL", "NOTIFY_ON", "NOTIFY_HEADER", "NOTIFY_TIMEOUT",
	"OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_CLIENT_SECRET_FILE", "OIDC_ALLOWED_EMAILS", "OIDC_OPERATORS",
	"OPERATOR_HANDLE", "SESSION_TTL", "SESSION_IDLE", "STREAM_POLL", "STREAM_HEARTBEAT",
}

func TestTableHoldsExactlyTheTwentyFiveProductVariables(t *testing.T) {
	var got []string
	for _, v := range All() {
		got = append(got, v.Suffix())
	}
	if !slices.Equal(got, r1Suffixes) {
		t.Fatalf("table suffixes = %v, want %v", got, r1Suffixes)
	}
	if len(got) != 25 {
		t.Fatalf("table has %d variables, want 25", len(got))
	}
}

func TestAllReturnsACopy(t *testing.T) {
	a := All()
	a[0] = Var{"CHANGED"}
	if All()[0].Suffix() != "URL" {
		t.Fatal("changing the slice All returned changed the table")
	}
}

// lookups records every name read, so a test can see which names are asked for.
type lookups struct {
	env  map[string]string
	read []string
}

func (l *lookups) get(name string) string {
	l.read = append(l.read, name)
	return l.env[name]
}

// TestEveryVariableResolvesVHThenBoardThenUnset is the R1 table test: all 24
// variables times the four cases.
func TestEveryVariableResolvesVHThenBoardThenUnset(t *testing.T) {
	for _, v := range All() {
		suffix := v.Suffix()
		vh, board := "VH_"+suffix, "BOARD_"+suffix
		cases := []struct {
			name string
			env  map[string]string
			want string
		}{
			{"only VH", map[string]string{vh: "from-vh"}, "from-vh"},
			{"only BOARD", map[string]string{board: "from-board"}, "from-board"},
			{"both: VH wins", map[string]string{vh: "from-vh", board: "from-board"}, "from-vh"},
			{"VH empty, BOARD set", map[string]string{vh: "", board: "from-board"}, "from-board"},
			{"neither", map[string]string{}, ""},
			{"both empty", map[string]string{vh: "", board: ""}, ""},
		}
		for _, tc := range cases {
			l := &lookups{env: tc.env}
			if got := v.Get(l.get); got != tc.want {
				t.Errorf("%s, %s: got %q, want %q", suffix, tc.name, got, tc.want)
			}
			for _, name := range l.read {
				if name != vh && name != board {
					t.Errorf("%s, %s: read %q, which is neither of its names", suffix, tc.name, name)
				}
			}
		}
	}
}

func TestVHIsReadFirstAndBoardOnlyWhenVHIsEmpty(t *testing.T) {
	l := &lookups{env: map[string]string{"VH_URL": "a", "BOARD_URL": "b"}}
	if got := URL.Get(l.get); got != "a" || !slices.Equal(l.read, []string{"VH_URL"}) {
		t.Fatalf("got %q after reading %v, want a after VH_URL alone", got, l.read)
	}
	l = &lookups{env: map[string]string{"BOARD_URL": "b"}}
	if got := URL.Get(l.get); got != "b" || !slices.Equal(l.read, []string{"VH_URL", "BOARD_URL"}) {
		t.Fatalf("got %q after reading %v, want b after VH_URL then BOARD_URL", got, l.read)
	}
}

func TestNamesAndMessages(t *testing.T) {
	if URL.Name() != "VH_URL" || URL.Alias() != "BOARD_URL" || URL.Suffix() != "URL" {
		t.Fatalf("URL names = %q %q %q", URL.Name(), URL.Alias(), URL.Suffix())
	}
	if got, want := URL.Unset(), "VH_URL is unset (BOARD_URL is also accepted)"; got != want {
		t.Fatalf("Unset() = %q, want %q", got, want)
	}
	if got, want := OIDCClientSecretFile.Name(), "VH_OIDC_CLIENT_SECRET_FILE"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestKnownNamesAreTheTableVariablesUnderEitherPrefix(t *testing.T) {
	for _, v := range All() {
		if !Known(v.Name()) || !Known(v.Alias()) {
			t.Errorf("%s / %s not known", v.Name(), v.Alias())
		}
	}
	for _, name := range []string{
		"", "VH_", "BOARD_", "URL", "VH_VERSION", "VH_REPO", "BOARD_CI_OUT", "BOARD_DB_LEG", "VH_URLS", "vh_url", "HOME", "BOARD_VH_URL",
	} {
		if Known(name) {
			t.Errorf("%q is known, want it outside the table", name)
		}
	}
}
