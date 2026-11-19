package cli

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// taggedTask is a task document whose own tags are the ones given.
func taggedTask(ref string, tags ...string) string {
	return fmt.Sprintf(`{"ref":%q,"initiative":"aa","title":"Tagged","kind":"work","status":"open","tags":[%s],"external_refs":[],"created_by":"c","created_at":"2026-01-01T11:55:00Z","active":false,"blocked":false,"overdue":false,"deps":[],"blocking":[],"live_leases":[]}`,
		ref, quotedList(tags))
}

func quotedList(tags []string) string {
	q := make([]string, len(tags))
	for i, t := range tags {
		q[i] = fmt.Sprintf("%q", t)
	}
	return strings.Join(q, ",")
}

func TestTaskAddSendsEachRepeatedTagInOrder(t *testing.T) {
	f := answering(201, taskJSON(1))
	code, out, body := sentBody(t, f, "task", "add", "aa", "Wire it", "--tag", "a", "--tag=b")
	want := map[string]any{"initiative": "aa", "title": "Wire it", "tags": []any{"a", "b"}}
	if code != ExitOK || out != "ok task aa-1 created\n" || !reflect.DeepEqual(body, want) {
		t.Fatalf("got %d %q %v, want %v", code, out, body, want)
	}
	if r := f.requests[0]; r.Method != "POST" || r.URL.Path != "/api/v1/tasks" {
		t.Errorf("%s %s", r.Method, r.URL.Path)
	}
	_, _, plain := sentBody(t, answering(201, taskJSON(1)), "task", "add", "aa", "Wire it")
	if _, has := plain["tags"]; has {
		t.Errorf("a task add without --tag sent tags: %v", plain)
	}
}

func TestNewSendsItsTagsInsideNewTask(t *testing.T) {
	f := answering(201, startAnswer(true, ""))
	code, _, body := sentBody(t, f, "new", "aa", "Fresh", "--start", "doing it", "--tag", "a")
	r := f.requests[0]
	nt, _ := body["new_task"].(map[string]any)
	want := map[string]any{"initiative": "aa", "title": "Fresh", "tags": []any{"a"}}
	if code != ExitOK || r.URL.Path != "/api/v1/start" || !reflect.DeepEqual(nt, want) {
		t.Fatalf("got %d %s new_task %v, want %v", code, r.URL.Path, nt, want)
	}
	if _, has := body["tags"]; has {
		t.Errorf("tags sent beside new_task: %v", body)
	}
	g := answering(201, startAnswer(true, ""))
	_, _, plain := sentBody(t, g, "new", "aa", "Fresh", "--start", "x")
	if nt, _ := plain["new_task"].(map[string]any); nt["tags"] != nil {
		t.Errorf("new without --tag sent tags: %v", nt)
	}
}

func TestUsageStringsShowTheTagFlagAndCommands(t *testing.T) {
	for name, usage := range map[string]string{"TaskUsage": TaskUsage, "NewUsage": NewUsage} {
		if !strings.Contains(usage, "--tag t]...") {
			t.Errorf("%s does not show --tag: %s", name, usage)
		}
	}
	if !strings.Contains(TaskUsage, "tag|untag <ref> <tag>...") {
		t.Errorf("TaskUsage does not show tag|untag: %s", TaskUsage)
	}
	for _, c := range []string{"task tag", "task untag"} {
		if got, ok := Commands()[c]; !ok || strings.Join(got, ",") != "--json" {
			t.Errorf("Commands()[%s] = %v", c, got)
		}
	}
	for _, c := range []string{"task add", "new"} {
		if !strings.Contains(strings.Join(Commands()[c], ","), "--tag") {
			t.Errorf("Commands()[%s] lacks --tag", c)
		}
	}
}

func TestTaskTagAndUntagCallTheEndpointOncePerTag(t *testing.T) {
	cases := []struct {
		args   []string
		method string
		out    string
		paths  []string
	}{
		{[]string{"task", "tag", "AA-1", "Work", "home"}, "PUT", "ok task aa-1 tagged work home\n", []string{"/api/v1/tasks/AA-1/tags/Work", "/api/v1/tasks/AA-1/tags/home"}},
		{[]string{"task", "untag", "aa-1", "work"}, "DELETE", "ok task aa-1 untagged work\n", []string{"/api/v1/tasks/aa-1/tags/work"}},
		{[]string{"task", "untag", "aa-1", "a", "b", "c"}, "DELETE", "ok task aa-1 untagged a b c\n", []string{"/api/v1/tasks/aa-1/tags/a", "/api/v1/tasks/aa-1/tags/b", "/api/v1/tasks/aa-1/tags/c"}},
		{[]string{"task", "tag", "--json", "aa-1", "a"}, "PUT", taggedTask("aa-1", "a") + "\n", []string{"/api/v1/tasks/aa-1/tags/a"}},
	}
	for _, c := range cases {
		f := answering(200, taggedTask("aa-1", "a"))
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), c.args...)
		srv.Close()
		var paths []string
		for _, r := range f.requests {
			if r.Method != c.method {
				t.Errorf("%v: method %s", c.args, r.Method)
			}
			paths = append(paths, r.URL.Path)
		}
		if code != ExitOK || out != c.out || errs != "" || !reflect.DeepEqual(paths, c.paths) {
			t.Errorf("%v: got %d %q %q %v, want %q %v", c.args, code, out, errs, paths, c.out, c.paths)
		}
	}
}

func TestTaskTagEscapesTheTagInThePath(t *testing.T) {
	f := answering(200, taggedTask("aa-1"))
	srv := httptest.NewServer(f)
	defer srv.Close()
	runEnv(f.env(srv), "task", "tag", "aa-1", "a/b")
	if got := f.requests[0].URL.EscapedPath(); got != "/api/v1/tasks/aa-1/tags/a%2Fb" {
		t.Errorf("path %s", got)
	}
}

func TestTaskTagUsageErrorsSendNothing(t *testing.T) {
	f := answering(200, taggedTask("aa-1"))
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, args := range [][]string{
		{"task", "tag"}, {"task", "tag", "aa-1"}, {"task", "untag"}, {"task", "untag", "aa-1"},
		{"task", "tag", "aa-1", "a", "--bogus"}, {"task", "untag", "aa-1", "a", "--tag", "b"},
	} {
		code, out, errs := runEnv(f.env(srv), args...)
		if code != ExitUsage || out != "" || errs != TaskUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("%d requests were sent for malformed command lines", len(f.requests))
	}
}

func TestTaskTagRefusalsWordTheirNextAndStopAtTheFirst(t *testing.T) {
	cases := []struct {
		status int
		detail string
		code   int
		want   string
	}{
		{http.StatusNotFound, "there is no task aa-9", ExitNotFound, "there is no task aa-9 → vh task ls\n"},
		{http.StatusConflict, "task aa-2 is closed", ExitConflict, "task aa-2 is closed → vh task show aa-2\n"},
		{http.StatusBadRequest, "bad tag", ExitError, "bad tag\n"},
	}
	for _, c := range cases {
		f := answering(c.status, `{"detail":"`+c.detail+`"}`)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), "task", "tag", "aa-2", "x", "y")
		srv.Close()
		if code != c.code || out != "" || errs != c.want || len(f.requests) != 1 {
			t.Errorf("%d: got %d %q %q with %d requests, want %d %q and 1", c.status, code, out, errs, len(f.requests), c.code, c.want)
		}
	}
}

func TestTagFailureKeepsANetworkErrorsWording(t *testing.T) {
	if got := tagFailure(http.ErrServerClosed, "aa-2"); !errors.Is(got, http.ErrServerClosed) {
		t.Errorf("got %v", got)
	}
}

func TestTaskLsPrintsEachTasksTags(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"items":[%s,%s]}`, taggedTask("aa-1", "home", "work"), taggedTask("aa-2"))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "task", "ls", "--attention")
	want := "aa-1  open  Tagged  #home #work\naa-2  open  Tagged\n"
	if code != ExitOK || errs != "" || out != want {
		t.Fatalf("got %d %q %q, want %q", code, out, errs, want)
	}
}

func TestTaskShowPrintsTheTasksTags(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		_, _ = w.Write([]byte(taggedTask("aa-1", "home", "work")))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "task", "show", "aa-1")
	if code != ExitOK || errs != "" || !strings.HasPrefix(out, "aa-1  open  Tagged  #home #work\ninitiative: aa\n") {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
}
