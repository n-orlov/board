package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixedNow(t *testing.T, now time.Time) {
	t.Helper()
	was := nowFn
	t.Cleanup(func() { nowFn = was })
	nowFn = func() time.Time { return now }
}

func TestMilestoneAndCheckPostAndConfirm(t *testing.T) {
	fixedNow(t, time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC))
	cases := []struct {
		args []string
		path string
		out  string
		want map[string]any
	}{
		{[]string{"milestone", "AA-1", "tests green"}, "/api/v1/tasks/AA-1/milestones", "ok task aa-1 milestone recorded\n", map[string]any{"summary": "tests green"}},
		{[]string{"check", "aa-1", "still up"}, "/api/v1/tasks/aa-1/checks", "ok task aa-1 checked\n", map[string]any{"summary": "still up"}},
		{[]string{"check", "aa-1", "ok", "--next", "24h"}, "/api/v1/tasks/aa-1/checks", "ok task aa-1 checked\n",
			map[string]any{"summary": "ok", "next_check_at": "2026-03-05T10:00:00Z"}},
		{[]string{"check", "--json", "aa-1", "ok"}, "/api/v1/tasks/aa-1/checks", taskJSON(1) + "\n", map[string]any{"summary": "ok"}},
	}
	for _, c := range cases {
		f := answering(200, taskJSON(1))
		code, out, body := sentBody(t, f, c.args...)
		if code != ExitOK || out != c.out || f.requests[0].Method != "POST" || f.requests[0].URL.Path != c.path {
			t.Errorf("%v: got %d %q %s", c.args, code, out, f.requests[0].URL.Path)
		}
		for k, v := range c.want {
			if body[k] != v {
				t.Errorf("%v: body %v, want %s = %v", c.args, body, k, v)
			}
		}
		if len(body) != len(c.want) {
			t.Errorf("%v: body %v has fields beyond %v", c.args, body, c.want)
		}
	}
}

func TestMilestoneAndCheckUsageErrors(t *testing.T) {
	cases := []struct {
		args  []string
		usage string
	}{
		{[]string{"milestone", "aa-1"}, MilestoneUsage},
		{[]string{"milestone", "aa-1", "x", "--next", "1h"}, MilestoneUsage},
		{[]string{"check", "aa-1", "x", "y"}, CheckUsage},
		{[]string{"check", "aa-1", "x", "--next"}, CheckUsage},
	}
	for _, c := range cases {
		code, out, errs := runEnv(func(string) string { return "" }, c.args...)
		if code != ExitUsage || out != "" || errs != c.usage+"\n" {
			t.Errorf("%v: got %d %q %q", c.args, code, out, errs)
		}
	}
	code, _, errs := runEnv(func(string) string { return "" }, "check", "aa-1", "x", "--next", "soon")
	if code != ExitUsage || !strings.HasPrefix(errs, "board: --next: invalid duration") {
		t.Errorf("bad --next: got %d %q", code, errs)
	}
}

func TestNoteRefusalsWordTheirNext(t *testing.T) {
	cases := []struct {
		status int
		code   int
		want   string
	}{
		{http.StatusNotFound, ExitNotFound, "d → board task ls\n"},
		{http.StatusConflict, ExitConflict, "d → board task show aa-1\n"},
		{http.StatusBadRequest, ExitError, "d\n"},
	}
	for _, c := range cases {
		f := answering(c.status, `{"detail":"d"}`)
		srv := httptest.NewServer(f)
		code, _, errs := runEnv(f.env(srv), "check", "AA-1", "x")
		srv.Close()
		if code != c.code || errs != c.want {
			t.Errorf("%d: got %d %q, want %d %q", c.status, code, errs, c.code, c.want)
		}
	}
}

const eventsPage = `{"items":[` +
	`{"ref":"E1","at":"2026-03-04T09:00:00Z","type":"task_created","actor":"a@x","summary":"created aa-1"},` +
	`{"ref":"E2","at":"2026-03-04T09:30:00Z","type":"milestone","actor":"b@x","summary":"milestone on aa-1: ok"}]}`

func TestEventsPrintsLinesAndSendsFilters(t *testing.T) {
	fixedNow(t, time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC))
	f := answering(200, eventsPage)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "events", "--since", "e5", "--task", "aa-1", "--initiative=aa",
		"--type", "milestone", "--type", "check_recorded", "--actor", "b@x")
	want := "E1  1h ago  a@x  created aa-1\nE2  30m ago  b@x  milestone on aa-1: ok\n"
	if code != ExitOK || out != want || errs != "" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	q := f.requests[0].URL.Query()
	if f.requests[0].URL.Path != "/api/v1/events" || q.Get("since") != "e5" || q.Get("task") != "aa-1" || q.Get("initiative") != "aa" ||
		q.Get("actor") != "b@x" || strings.Join(q["type"], ",") != "milestone,check_recorded" {
		t.Errorf("request %s", f.requests[0].URL)
	}
}

func TestEventsSinceDateAndTaskEventsPath(t *testing.T) {
	f := answering(200, eventsPage)
	srv := httptest.NewServer(f)
	defer srv.Close()
	runEnv(f.env(srv), "events", "--since", "2026-03-04")
	if got := f.requests[0].URL.Query().Get("since"); got != "2026-03-04T00:00:00Z" {
		t.Errorf("since %q", got)
	}
	code, out, _ := runEnv(f.env(srv), "task", "events", "AA-1", "--json")
	if f.requests[1].URL.Path != "/api/v1/tasks/AA-1/events" || code != ExitOK || !strings.HasPrefix(out, `[{"ref":"E1"`) {
		t.Errorf("got %d %q %s", code, out, f.requests[1].URL.Path)
	}
}

func TestEventsCapsAndNamesTheFilters(t *testing.T) {
	f := answering(200, eventsPage)
	srv := httptest.NewServer(f)
	defer srv.Close()
	_, out, _ := runEnv(f.env(srv), "events", "--limit", "1", "--task", "aa-1", "--type", "milestone", "--actor", "a b", "--since", "E3", "--initiative", "aa")
	last := out[strings.LastIndex(strings.TrimSuffix(out, "\n"), "\n")+1:]
	if want := "+1 more → board events --since E3 --task aa-1 --initiative aa --actor \"a b\" --type milestone --all\n"; last != want {
		t.Errorf("last line %q, want %q", last, want)
	}
	_, out, _ = runEnv(f.env(srv), "task", "events", "AA-1", "--limit=1")
	if !strings.HasSuffix(out, "+1 more → board task events aa-1 --all\n") {
		t.Errorf("task events: %q", out)
	}
}

func TestEventsPagesAndRefusals(t *testing.T) {
	calls := 0
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("after") == "" {
			_, _ = w.Write([]byte(`{"items":[{"ref":"E1","at":"2026-03-04T09:00:00Z","actor":"a","summary":"s"}],"next_after":"E1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"ref":"E2","at":"2026-03-04T09:00:00Z","actor":"a","summary":"s"}]}`))
	}}
	srv := httptest.NewServer(f)
	_, out, _ := runEnv(f.env(srv), "events", "--json")
	srv.Close()
	if calls != 2 || !strings.Contains(out, `"E1"`) || !strings.Contains(out, `"E2"`) {
		t.Errorf("calls %d out %q", calls, out)
	}
	cases := []struct {
		detail string
		want   string
	}{
		{"there is no initiative zz", "there is no initiative zz → board init ls\n"},
		{"there is no task zz-1", "there is no task zz-1 → board task ls\n"},
	}
	for _, c := range cases {
		fb := answering(404, `{"detail":"`+c.detail+`"}`)
		srv := httptest.NewServer(fb)
		code, _, errs := runEnv(fb.env(srv), "events", "--task", "zz-1")
		srv.Close()
		if code != ExitNotFound || errs != c.want {
			t.Errorf("%s: got %d %q", c.detail, code, errs)
		}
	}
}

func TestEventsUsageErrors(t *testing.T) {
	none := func(string) string { return "" }
	for _, args := range [][]string{{"events", "extra"}, {"events", "--limit", "0"}, {"events", "--all", "--limit", "3"}, {"events", "--bogus"}} {
		if code, _, errs := runEnv(none, args...); code != ExitUsage || errs != EventsUsage+"\n" {
			t.Errorf("%v: got %d %q", args, code, errs)
		}
	}
	for _, args := range [][]string{{"task", "events"}, {"task", "events", "a", "b"}} {
		if code, _, errs := runEnv(none, args...); code != ExitUsage || errs != TaskUsage+"\n" {
			t.Errorf("%v: got %d %q", args, code, errs)
		}
	}
	if code, _, errs := runEnv(none, "events", "--since", "2026-13-45"); code != ExitUsage || !strings.HasPrefix(errs, "board: --since: invalid time") {
		t.Errorf("bad date: %d %q", code, errs)
	}
}

func TestTheCommandTreeHasTheEventCommandsWithTheirFlags(t *testing.T) {
	want := map[string]string{
		"milestone":   "--json",
		"check":       "--json --next",
		"task events": "--all --json --limit",
		"events":      "--actor --all --initiative --json --limit --since --task --type",
	}
	tree := Commands()
	for cmd, flags := range want {
		got, ok := tree[cmd]
		if !ok || strings.Join(got, " ") != flags {
			t.Errorf("%q: flags %v (present %v), want %q", cmd, got, ok, flags)
		}
	}
}
