package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskDepCallsItsPathAndPrintsTheOkLine(t *testing.T) {
	cases := []struct {
		args    []string
		method  string
		blocker string
		out     string
	}{
		{[]string{"dep", "add", "aa-2", "AA-1"}, "POST", "AA-1", "ok task aa-1 blocked by aa-1\n"},
		{[]string{"dep", "rm", "aa-2", "aa-1"}, "DELETE", "aa-1", "ok task aa-1 no longer blocked by aa-1\n"},
		{[]string{"dep", "add", "--json", "aa-2", "aa-1"}, "POST", "aa-1", taskJSON(1) + "\n"},
	}
	for _, c := range cases {
		f := answering(200, taskJSON(1))
		code, out, _ := sentBody(t, f, append([]string{"task"}, c.args...)...)
		r := f.requests[0]
		if code != ExitOK || out != c.out || r.Method != c.method || r.URL.Path != "/api/v1/tasks/aa-2/deps/"+c.blocker {
			t.Errorf("%v: got %d %q %s %s", c.args, code, out, r.Method, r.URL.Path)
		}
	}
}

func TestTaskDepRefusalsWordTheirNext(t *testing.T) {
	cases := []struct {
		status int
		detail string
		code   int
		want   string
	}{
		{http.StatusNotFound, "there is no task aa-9", ExitNotFound, "there is no task aa-9 → vh task ls\n"},
		{http.StatusNotFound, "task aa-2 is not blocked by aa-1", ExitNotFound, "task aa-2 is not blocked by aa-1 → vh task show aa-2\n"},
		{http.StatusConflict, "cycle", ExitConflict, "cycle → vh task show aa-2\n"},
		{http.StatusBadRequest, "self", ExitError, "self\n"},
	}
	for _, c := range cases {
		f := answering(c.status, `{"detail":"`+c.detail+`"}`)
		srv := httptest.NewServer(f)
		code, _, errs := runEnv(f.env(srv), "task", "dep", "rm", "aa-2", "aa-1")
		srv.Close()
		if code != c.code || errs != c.want {
			t.Errorf("%d: got %d %q, want %d %q", c.status, code, errs, c.code, c.want)
		}
	}
}

func TestTaskDepNetworkErrorKeepsItsWording(t *testing.T) {
	if got := depFailure(http.ErrServerClosed, "aa-2"); !errors.Is(got, http.ErrServerClosed) {
		t.Errorf("got %v", got)
	}
}

func TestTaskDepUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"task", "dep"}, {"task", "dep", "frob", "a", "b"}, {"task", "dep", "add", "a"}, {"task", "dep", "add", "a", "b", "c"},
		{"task", "dep", "rm", "a", "b", "--bogus"},
	} {
		code, out, errs := runEnv(envWith(map[string]string{}), args...)
		if code != ExitUsage || out != "" || errs != TaskUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}
