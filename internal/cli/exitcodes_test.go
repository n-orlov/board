package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// phase0Exit is the set of exit codes Phase 0 may produce (SPEC §8.1, R9).
var phase0Exit = map[int]bool{ExitOK: true, ExitError: true, ExitUsage: true, ExitNotFound: true, ExitAuth: true}

// TestPhase0CommandsOnlyProduceAllowedExitCodes runs every Phase 0 client
// command against a server answering each error status, a dead server, and
// bad invocations, and requires the exit code to stay in {0,1,2,4,5}.
func TestPhase0CommandsOnlyProduceAllowedExitCodes(t *testing.T) {
	commands := [][]string{
		{"whoami"},
		{"init", "add", "xx", "T"},
		{"init", "add", "xx", "T", "--tag", "a", "--json"},
		{"init", "ls"},
		{"init", "ls", "--tag", "a", "--all"},
		{"init", "show", "xx"},
		{"init", "show", "xx", "--json"},
	}
	for _, status := range []int{400, 401, 403, 404, 405, 409, 410, 412, 418, 422, 423, 429, 500, 502, 503} {
		f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"title":"x","detail":"refused","next":"try again"}`))
		}}
		srv := httptest.NewServer(f)
		for _, args := range commands {
			code, _, _ := runEnv(f.env(srv), args...)
			if !phase0Exit[code] {
				t.Errorf("%v with status %d: exit %d outside {0,1,2,4,5}", args, status, code)
			}
		}
		srv.Close()
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	f := &fakeBoard{}
	for _, args := range commands {
		if code, _, _ := runEnv(f.env(dead), args...); code != ExitError {
			t.Errorf("%v against a dead server: exit %d, want %d", args, code, ExitError)
		}
		if code, _, _ := runEnv(envWith(nil), args...); !phase0Exit[code] {
			t.Errorf("%v with no environment: exit %d", args, code)
		}
	}
	for _, args := range [][]string{{}, {"bogus"}, {"init"}, {"init", "add"}, {"init", "ls", "--limit", "x"}, {"init", "show"}, {"whoami", "extra"}} {
		if code, _, _ := runEnv(envWith(nil), args...); !phase0Exit[code] {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

// TestDuplicateInitiativeExitsOneForAnyConflictStatus pins that a 409 is a
// plain error (exit 1) with its one-line next action, whatever command meets it.
func TestDuplicateInitiativeExitsOneForAnyConflictStatus(t *testing.T) {
	for _, args := range [][]string{{"init", "add", "xx", "T"}, {"init", "show", "xx"}, {"init", "ls"}, {"whoami"}} {
		f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"title":"Conflict","detail":"taken","next":"read it"}`))
		}}
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), args...)
		srv.Close()
		if code != ExitError || out != "" || strings.Count(errs, "\n") != 1 || !strings.HasSuffix(errs, "\n") {
			t.Errorf("%v: exit %d out %q err %q", args, code, out, errs)
		}
	}
}
