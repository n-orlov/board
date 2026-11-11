package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func taskJSON(i int) string {
	return fmt.Sprintf(`{"id":"01T%d","ref":"aa-%d","initiative":"aa","seq":%d,"title":"Task %d","description":"","kind":"work","status":"open","external_refs":[],"created_by":"a","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","active":false,"blocked":false,"overdue":false,"deps":[],"blocking":[],"live_leases":[]}`, i, i, i, i)
}

func TestTaskAddSendsTheFlagsGiven(t *testing.T) {
	cases := []struct {
		args []string
		want map[string]any
	}{
		{nil, map[string]any{"initiative": "aa", "title": "Wire it"}},
		{[]string{"--kind", "watch", "--cadence", "24h"}, map[string]any{"kind": "watch", "cadence_seconds": float64(86400)}},
		{[]string{"--after", "aa-1", "--after=bb-2"}, map[string]any{"blocked_by": []any{"aa-1", "bb-2"}}},
		{[]string{"--start", "2026-01-02", "--end", "2026-01-03T10:00:00Z"}, map[string]any{"planned_start": "2026-01-02T00:00:00Z", "planned_end": "2026-01-03T10:00:00Z"}},
		{[]string{"--link", "PR=https://e.com/1", "--link", "CI=https://e.com/2"}, map[string]any{"external_refs": []any{
			map[string]any{"label": "PR", "url": "https://e.com/1"}, map[string]any{"label": "CI", "url": "https://e.com/2"}}}},
	}
	for _, c := range cases {
		f := answering(201, taskJSON(1))
		code, out, body := sentBody(t, f, append([]string{"task", "add", "aa", "Wire it"}, c.args...)...)
		want := map[string]any{"initiative": "aa", "title": "Wire it"}
		for k, v := range c.want {
			want[k] = v
		}
		if code != ExitOK || out != "ok task aa-1 created\n" || !reflect.DeepEqual(body, want) {
			t.Errorf("%v: got %d %q %v, want %v", c.args, code, out, body, want)
		}
		if r := f.requests[0]; r.Method != "POST" || r.URL.Path != "/api/v1/tasks" {
			t.Errorf("%v: %s %s", c.args, r.Method, r.URL.Path)
		}
	}
}

func TestTaskFlagsComeBeforeOrAfterThePositionals(t *testing.T) {
	for _, args := range [][]string{
		{"task", "add", "--kind", "watch", "--cadence=2h", "aa", "Wire it"},
		{"task", "add", "aa", "--kind=watch", "Wire it", "--cadence", "2h"},
	} {
		code, out, body := sentBody(t, answering(201, taskJSON(1)), args...)
		if code != ExitOK || out != "ok task aa-1 created\n" || body["cadence_seconds"] != float64(7200) || body["title"] != "Wire it" {
			t.Errorf("%v: got %d %q %v", args, code, out, body)
		}
	}
}

func TestTaskAddRefusesBadFlagValuesWithExit2(t *testing.T) {
	for _, c := range [][]string{
		{"--cadence", "5"}, {"--cadence", "-5m"}, {"--cadence", "abc"}, {"--start", "tomorrow"}, {"--end", "2026-13-01"},
		{"--link", "nolabel"}, {"--kind", "chore"},
	} {
		f := answering(201, taskJSON(1))
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), append([]string{"task", "add", "aa", "T"}, c...)...)
		srv.Close()
		if code != ExitUsage || out != "" || !strings.HasPrefix(errs, "board: "+c[0]+": ") || strings.Count(errs, "\n") != 1 || len(f.requests) != 0 {
			t.Errorf("%v: got %d %q %q with %d requests", c, code, out, errs, len(f.requests))
		}
	}
}

func TestTaskUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"task"}, {"task", "frob"}, {"task", "add", "aa"}, {"task", "add", "aa", "T", "extra"}, {"task", "add", "aa", "T", "--bogus"},
		{"task", "ls", "a", "b"}, {"task", "ls", "--status", "weird"}, {"task", "ls", "--kind", "x"}, {"task", "ls", "--limit", "0"},
		{"task", "ls", "--all", "--limit", "2"}, {"task", "show"}, {"task", "show", "a", "b"}, {"task", "edit", "aa-1"},
		{"task", "edit", "aa-1", "--link", "L=http://x", "--no-links"}, {"task", "edit", "--title", "T"},
	} {
		code, out, errs := runEnv(envWith(map[string]string{}), args...)
		if code != ExitUsage || out != "" || errs != TaskUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestTaskEditSendsOnlyTheFlagsGiven(t *testing.T) {
	file := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(file, []byte("body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args []string
		want map[string]any
	}{
		{[]string{"--title", "New"}, map[string]any{"title": "New"}},
		{[]string{"--description", file}, map[string]any{"description": "body\n"}},
		{[]string{"--cadence", "90m", "--start", "2026-05-06"}, map[string]any{"cadence_seconds": float64(5400), "planned_start": "2026-05-06T00:00:00Z"}},
		{[]string{"--no-links"}, map[string]any{"external_refs": []any{}}},
		{[]string{"--link", "A=http://a"}, map[string]any{"external_refs": []any{map[string]any{"label": "A", "url": "http://a"}}}},
	}
	for _, c := range cases {
		f := answering(200, taskJSON(1))
		code, out, body := sentBody(t, f, append([]string{"task", "edit", "AA-1"}, c.args...)...)
		if code != ExitOK || out != "ok task aa-1 updated\n" || !reflect.DeepEqual(body, c.want) {
			t.Errorf("%v: got %d %q %v, want %v", c.args, code, out, body, c.want)
		}
		if r := f.requests[0]; r.Method != "PATCH" || r.URL.Path != "/api/v1/tasks/AA-1" {
			t.Errorf("%v: %s %s", c.args, r.Method, r.URL.Path)
		}
	}
}

func TestTaskWriteFailuresNameTheNextAction(t *testing.T) {
	cases := []struct {
		args       []string
		status     int
		detail     string
		code       int
		wantStderr string
	}{
		{[]string{"add", "zz", "T"}, 404, "there is no initiative zz", ExitNotFound, "there is no initiative zz → board init ls\n"},
		{[]string{"add", "aa", "T", "--after", "aa-9"}, 404, "task not found: aa-9", ExitNotFound, "task not found: aa-9 → board task ls aa\n"},
		{[]string{"add", "aa", "T"}, 409, "initiative aa is closed", ExitConflict, "initiative aa is closed → board init show aa\n"},
		{[]string{"add", "aa", ""}, 400, "title is empty", ExitError, "title is empty → server next\n"},
		{[]string{"edit", "aa-9", "--title", "X"}, 404, "task not found: aa-9", ExitNotFound, "task not found: aa-9 → board task ls\n"},
		{[]string{"edit", "aa-1", "--title", "X"}, 409, "task aa-1 is closed", ExitConflict, "task aa-1 is closed → board task show aa-1\n"},
	}
	for _, c := range cases {
		f := answering(c.status, fmt.Sprintf(`{"title":"x","detail":%q,"next":"server next"}`, c.detail))
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), append([]string{"task"}, c.args...)...)
		srv.Close()
		if code != c.code || out != "" || errs != c.wantStderr {
			t.Errorf("%v: got %d %q %q, want %d %q", c.args, code, out, errs, c.code, c.wantStderr)
		}
	}
}

// taskPages serves GET /api/v1/tasks in pages of 7 out of total tasks aa-1...
// None of them needs attention, so attention=true answers an empty page.
func taskPages(total int) *fakeBoard {
	return &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("attention") == "true" {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		after := 0
		if a := r.URL.Query().Get("after"); a != "" {
			_, _ = fmt.Sscanf(a, "aa-%d", &after)
		}
		var items []string
		for i := after + 1; i <= total && len(items) < 7; i++ {
			items = append(items, taskJSON(i))
		}
		next := ""
		if after+len(items) < total {
			next = fmt.Sprintf(`,"next_after":"aa-%d"`, after+len(items))
		}
		_, _ = fmt.Fprintf(w, `{"items":[%s]%s}`, strings.Join(items, ","), next)
	}}
}

func TestTaskLsCapsAndNamesTheFiltersInTheMoreLine(t *testing.T) {
	f := taskPages(21)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "task", "ls")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != ExitOK || len(lines) != 21 || lines[0] != "aa-1  open  Task 1" || lines[20] != "+1 more → board task ls --all" {
		t.Fatalf("got %d %d lines, last %q", code, len(lines), lines[len(lines)-1])
	}
	_, out, _ = runEnv(f.env(srv), "task", "ls", "aa", "--status", "all", "--kind", "watch", "--blocked", "--active", "--overdue", "--q", "two words", "--limit", "2")
	if want := "+19 more → board task ls aa --status all --kind watch --active --blocked --overdue --q \"two words\" --all\n"; !strings.HasSuffix(out, want) {
		t.Fatalf("limit 2: %q", out)
	}
	q := f.requests[len(f.requests)-1].URL.Query()
	for k, v := range map[string]string{"initiative": "aa", "status": "all", "kind": "watch", "active": "true", "blocked": "true", "overdue": "true", "q": "two words"} {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), v)
		}
	}
	code, out, _ = runEnv(f.env(srv), "task", "ls", "--all", "--json")
	if code != ExitOK || strings.Count(out, `"ref":"aa-`) != 21 {
		t.Fatalf("--all --json: %d %q", code, out)
	}
}

func TestTaskLineMarksWhatIsDerived(t *testing.T) {
	line := taskLine(task{Ref: "aa-3", Status: "open", Title: "Check it", Kind: "watch", Blocked: true, Active: true, Overdue: true})
	if line != "aa-3  open  Check it  (watch, blocked, active, overdue)" {
		t.Fatalf("got %q", line)
	}
}

func TestTaskShowPrintsFactsDepsLeasesAndTheLastTenEvents(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	was := nowFn
	defer func() { nowFn = was }()
	nowFn = func() time.Time { return now }
	events := func(w http.ResponseWriter, r *http.Request) {
		after := 0
		if a := r.URL.Query().Get("after"); a != "" {
			_, _ = fmt.Sscanf(a, "E%d", &after)
		}
		var items []string
		for i := after + 1; i <= 25 && len(items) < 10; i++ {
			items = append(items, fmt.Sprintf(`{"ref":"E%d","at":"2026-01-01T11:00:00Z","actor":"a","summary":"event %d"}`, i, i))
		}
		next := ""
		if after+len(items) < 25 {
			next = fmt.Sprintf(`,"next_after":"E%d"`, after+len(items))
		}
		_, _ = fmt.Fprintf(w, `{"items":[%s]%s}`, strings.Join(items, ","), next)
	}
	task := `{"ref":"aa-2","initiative":"aa","title":"Watch it","description":"The text","kind":"watch","status":"open","cadence_seconds":86400,"next_check_at":"2026-01-01T14:00:00Z",
	"planned_start":"2026-01-02T00:00:00Z","planned_end":"2026-01-05T10:00:00Z","external_refs":[{"label":"PR","url":"https://e.com/1"}],"created_by":"c","created_at":"2026-01-01T11:55:00Z",
	"active":true,"blocked":true,"overdue":false,"deps":[{"ref":"aa-1","title":"First","status":"open"}],"blocking":[{"ref":"aa-3","title":"Third","status":"open"}],
	"live_leases":[{"ref":"L7","actor":"x@y","intent":"doing it","expires_at":"2026-01-01T13:00:00Z"}]}`
	task = strings.NewReplacer("\n", "", "\t", "").Replace(task)
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			events(w, r)
			return
		}
		_, _ = w.Write([]byte(task))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "task", "show", "aa-2")
	want := `aa-2  open  Watch it  (watch, blocked, active)
initiative: aa
planned: 2026-01-02 → 2026-01-05T10:00:00Z
cadence: 1d, next check in 2h
link: PR https://e.com/1
created by c 5m ago
blocked by aa-1 (open) First
blocking aa-3 (open) Third
lease L7 by x@y: doing it (expires in 1h)
The text
events:
`
	for i := 16; i <= 25; i++ {
		want += fmt.Sprintf("  E%d  1h ago  a  event %d\n", i, i)
	}
	if code != ExitOK || errs != "" || out != want {
		t.Fatalf("got %d %q\n%s\nwant\n%s", code, errs, out, want)
	}
	_, out, _ = runEnv(f.env(srv), "task", "show", "aa-2", "--json")
	if !strings.HasPrefix(out, `{"ref":"aa-2"`) || strings.Count(out, "\n") != 1 {
		t.Fatalf("--json: %q", out)
	}
}

func TestUntilTimeAndShortTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"2026-01-01T12:00:30Z": "in under 1m", "2026-01-01T12:05:00Z": "in 5m", "2026-01-02T12:00:00Z": "in 24h",
		"2026-01-05T12:00:00Z": "in 4d", "2026-01-01T11:00:00Z": "1h ago", "junk": "junk",
	} {
		if got := untilTime(now, in); got != want {
			t.Errorf("untilTime(%q) = %q, want %q", in, got, want)
		}
	}
	if shortTime("2026-01-02T00:00:00Z") != "2026-01-02" || shortTime("x") != "x" || shortTime("2026-01-02T01:00:00Z") != "2026-01-02T01:00:00Z" {
		t.Error("shortTime")
	}
}
