package cli

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTaskTransitionsPostToTheirPath(t *testing.T) {
	cases := []struct {
		args []string
		path string
		want map[string]any
		out  string
	}{
		{[]string{"done", "aa-1"}, "/api/v1/tasks/aa-1/done", nil, "ok task aa-1 done\n"},
		{[]string{"done", "aa-1", "shipped"}, "/api/v1/tasks/aa-1/done", map[string]any{"outcome": "shipped"}, "ok task aa-1 done\n"},
		{[]string{"done", "aa-1", "--force"}, "/api/v1/tasks/aa-1/done", map[string]any{"force": true}, "ok task aa-1 done\n"},
		{[]string{"done", "aa-1", "shipped", "--force"}, "/api/v1/tasks/aa-1/done", map[string]any{"outcome": "shipped", "force": true}, "ok task aa-1 done\n"},
		{[]string{"reopen", "aa-1"}, "/api/v1/tasks/aa-1/reopen", nil, "ok task aa-1 reopened\n"},
		{[]string{"close", "aa-1"}, "/api/v1/tasks/aa-1/close", map[string]any{"resolution": "completed"}, "ok task aa-1 closed\n"},
		{[]string{"close", "aa-1", "--resolution", "superseded"}, "/api/v1/tasks/aa-1/close", map[string]any{"resolution": "superseded"}, "ok task aa-1 closed\n"},
	}
	for _, c := range cases {
		f := answering(200, taskJSON(1))
		code, out, body := sentBody(t, f, append([]string{"task"}, c.args...)...)
		r := f.requests[0]
		if code != ExitOK || out != c.out || !reflect.DeepEqual(body, c.want) || r.Method != "POST" || r.URL.Path != c.path {
			t.Errorf("%v: got %d %q %v %s %s", c.args, code, out, body, r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Board-On-Behalf-Of"); got != "" {
			t.Errorf("%v: Board-On-Behalf-Of %q, want none", c.args, got)
		}
	}
}

func TestTaskCloseForOperatorSendsTheHeader(t *testing.T) {
	f := answering(200, taskJSON(1))
	code, out, _ := sentBody(t, f, "task", "close", "aa-1", "--for", "operator")
	if code != ExitOK || out != "ok task aa-1 closed\n" {
		t.Fatalf("got %d %q", code, out)
	}
	if got := f.requests[0].Header.Get("Board-On-Behalf-Of"); got != "operator" {
		t.Errorf("Board-On-Behalf-Of %q, want operator", got)
	}
}

func TestTaskTransitionJSONPrintsTheObject(t *testing.T) {
	code, out, _ := sentBody(t, answering(200, taskJSON(1)), "task", "done", "aa-1", "--json")
	if code != ExitOK || out != taskJSON(1)+"\n" {
		t.Errorf("got %d %q", code, out)
	}
}

func TestTaskTransitionRefusalsWordTheirNext(t *testing.T) {
	cases := []struct {
		status int
		code   int
		want   string
	}{
		{http.StatusNotFound, ExitNotFound, "gone → board task ls\n"},
		{http.StatusConflict, ExitConflict, "gone → board task show aa-1\n"},
		{http.StatusBadRequest, ExitError, "gone\n"},
	}
	for _, c := range cases {
		f := answering(c.status, `{"detail":"gone"}`)
		srv := httptest.NewServer(f)
		code, _, errs := runEnv(f.env(srv), "task", "done", "aa-1")
		srv.Close()
		if code != c.code || errs != c.want {
			t.Errorf("%d: got %d %q, want %d %q", c.status, code, errs, c.code, c.want)
		}
	}
}

func TestTaskTransitionBadFlagsExit2(t *testing.T) {
	for _, args := range [][]string{
		{"task", "done"},
		{"task", "done", "aa-1", "aa-2", "aa-3"},
		{"task", "reopen", "aa-1", "outcome"},
		{"task", "close", "aa-1", "outcome"},
		{"task", "reopen", "aa-1", "--force"},
		{"task", "reopen", "aa-1", "--resolution", "dropped"},
		{"task", "done", "aa-1", "--for", "operator"},
		{"task", "close", "aa-1", "--resolution"},
		{"task", "close", "aa-1", "--resolution", "finished"},
		{"task", "close", "aa-1", "--for", "someone"},
	} {
		if code, out, _ := run(args...); code != ExitUsage || out != "" {
			t.Errorf("%v: got %d %q, want exit 2", args, code, out)
		}
	}
}

func TestTaskDoneRefusedByAnotherLeaseKeepsTheServersNext(t *testing.T) {
	f := answering(http.StatusConflict, `{"detail":"task aa-1 is leased by codex","next":"wait for L1 to end, or --force"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "task", "done", "aa-1")
	if code != ExitConflict || out != "" || errs != "task aa-1 is leased by codex → wait for L1 to end, or --force\n" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
	for _, verb := range []string{"reopen", "close"} {
		g := answering(http.StatusConflict, `{"detail":"no","next":"wait for L1 to end, or --force"}`)
		s := httptest.NewServer(g)
		_, _, errs := runEnv(g.env(s), "task", verb, "aa-1")
		s.Close()
		if errs != "no → board task show aa-1\n" {
			t.Errorf("%s: got %q", verb, errs)
		}
	}
}
