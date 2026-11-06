package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// answering is a fake board that answers every request with status and body.
func answering(status int, body string) *fakeBoard {
	return &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}}
}

// sentBody runs the command against f and returns the JSON body of its one request.
func sentBody(t *testing.T, f *fakeBoard, args ...string) (int, string, map[string]any) {
	t.Helper()
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), args...)
	if errs != "" {
		t.Fatalf("%v: stderr %q", args, errs)
	}
	if len(f.bodies) != 1 {
		t.Fatalf("%v: %d requests, want 1", args, len(f.bodies))
	}
	var body map[string]any
	if f.bodies[0] != "" {
		if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil {
			t.Fatalf("%v: body %q: %v", args, f.bodies[0], err)
		}
	}
	return code, out, body
}

func TestInitEditSendsOnlyTheFlagsGiven(t *testing.T) {
	cases := []struct {
		args []string
		want map[string]any
	}{
		{[]string{"--title", "New title"}, map[string]any{"title": "New title"}},
		{[]string{"--title=A=B"}, map[string]any{"title": "A=B"}},
		{[]string{"--tag", "work", "--tag", "lab"}, map[string]any{"tags": []any{"work", "lab"}}},
		{[]string{"--no-tags"}, map[string]any{"tags": []any{}}},
		{[]string{"--start", "2026-01-02", "--end", "2026-03-04"}, map[string]any{"target_start": "2026-01-02", "target_end": "2026-03-04"}},
		{[]string{"--clear-start"}, map[string]any{"target_start": nil}},
		{[]string{"--clear-end", "--title", "T"}, map[string]any{"target_end": nil, "title": "T"}},
	}
	for _, c := range cases {
		f := answering(200, initiativeJSON(1))
		code, out, body := sentBody(t, f, append([]string{"init", "edit", "slug-01"}, c.args...)...)
		if code != ExitOK || out != "ok initiative slug-01 updated\n" {
			t.Errorf("%v: got %d %q", c.args, code, out)
		}
		if !reflect.DeepEqual(body, c.want) {
			t.Errorf("%v: body %v, want %v", c.args, body, c.want)
		}
		if r := f.requests[0]; r.Method != "PATCH" || r.URL.Path != "/api/v1/initiatives/slug-01" {
			t.Errorf("%v: %s %s", c.args, r.Method, r.URL.Path)
		}
	}
}

func TestInitEditReadsTheDescriptionFromAFileOrStandardInput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "d.md")
	if err := os.WriteFile(file, []byte("# From a file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	was := stdin
	defer func() { stdin = was }()
	stdin = strings.NewReader("from stdin\n")
	for src, want := range map[string]string{file: "# From a file\n", "-": "from stdin\n"} {
		code, out, body := sentBody(t, answering(200, initiativeJSON(1)), "init", "edit", "slug-01", "--description", src)
		if code != ExitOK || out != "ok initiative slug-01 updated\n" || !reflect.DeepEqual(body, map[string]any{"description": want}) {
			t.Errorf("%s: got %d %q %v", src, code, out, body)
		}
	}
}

func TestInitEditDescriptionFailuresExitOne(t *testing.T) {
	env := envWith(map[string]string{"BOARD_URL": "http://127.0.0.1:1", "BOARD_API_KEY": "brd_k"})
	code, out, errs := runEnv(env, "init", "edit", "ab", "--description", filepath.Join(t.TempDir(), "missing.md"))
	if code != ExitError || out != "" || !strings.HasPrefix(errs, "cannot read the description file ") || strings.Count(errs, "\n") != 1 {
		t.Errorf("missing file: %d %q %q", code, out, errs)
	}
	was := stdin
	defer func() { stdin = was }()
	stdin = errReader{}
	code, out, errs = runEnv(env, "init", "edit", "ab", "--description", "-")
	if code != ExitError || out != "" || !strings.HasPrefix(errs, "cannot read the description from -: ") {
		t.Errorf("failing stdin: %d %q %q", code, out, errs)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestInitEditJSONPrintsTheObject(t *testing.T) {
	code, out, _ := sentBody(t, answering(200, initiativeJSON(1)), "init", "edit", "slug-01", "--title", "T", "--json")
	if code != ExitOK || strings.TrimSpace(out) != initiativeJSON(1) {
		t.Errorf("got %d %q", code, out)
	}
}

func TestInitWriteUsageErrors(t *testing.T) {
	env := envWith(map[string]string{"BOARD_URL": "http://127.0.0.1:1", "BOARD_API_KEY": "brd_k"})
	for _, args := range [][]string{
		{"init", "edit"}, {"init", "edit", "ab"}, {"init", "edit", "ab", "cd", "--title", "T"}, {"init", "edit", "ab", "--json"},
		{"init", "edit", "ab", "--tag", "x", "--no-tags"}, {"init", "edit", "ab", "--start", "2026-01-01", "--clear-start"},
		{"init", "edit", "ab", "--end", "2026-01-01", "--clear-end"}, {"init", "edit", "ab", "--title"}, {"init", "edit", "ab", "--slug", "x"},
		{"init", "edit", "ab", "--no-tags=yes"},
		{"init", "close"}, {"init", "close", "ab", "cd"}, {"init", "close", "ab", "--forced"}, {"init", "close", "ab", "--force=1"},
		{"init", "reopen"}, {"init", "reopen", "ab", "--force"}, {"init", "reopen", "ab", "cd"},
		{"init", "ls", "--status", "bogus"}, {"init", "ls", "--closed-within", "7"}, {"init", "ls", "--status", "open", "--closed-within", "7"},
		{"init", "ls", "--status", "closed", "--closed-within", "0"}, {"init", "ls", "--status", "closed", "--closed-within", "x"},
		{"init", "ls", "--status", "all", "--closed-within", "-3"}, {"init", "ls", "--status"},
	} {
		code, out, errs := runEnv(env, args...)
		if code != ExitUsage || out != "" || errs != InitUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestInitCloseAndReopenConfirm(t *testing.T) {
	code, out, body := sentBody(t, answering(200, initiativeJSON(1)), "init", "close", "slug-01")
	if code != ExitOK || out != "ok initiative slug-01 closed\n" || len(body) != 0 {
		t.Errorf("close: %d %q %v", code, out, body)
	}
	f := answering(200, initiativeJSON(1))
	code, out, body = sentBody(t, f, "init", "close", "slug-01", "--force")
	if code != ExitOK || out != "ok initiative slug-01 closed\n" || !reflect.DeepEqual(body, map[string]any{"force": true}) {
		t.Errorf("close --force: %d %q %v", code, out, body)
	}
	if r := f.requests[0]; r.Method != "POST" || r.URL.Path != "/api/v1/initiatives/slug-01/close" {
		t.Errorf("close: %s %s", r.Method, r.URL.Path)
	}
	f = answering(200, initiativeJSON(1))
	code, out, _ = sentBody(t, f, "init", "reopen", "slug-01")
	if code != ExitOK || out != "ok initiative slug-01 reopened\n" || f.requests[0].URL.Path != "/api/v1/initiatives/slug-01/reopen" {
		t.Errorf("reopen: %d %q %s", code, out, f.requests[0].URL.Path)
	}
	code, out, _ = sentBody(t, answering(200, initiativeJSON(1)), "init", "reopen", "slug-01", "--json")
	if code != ExitOK || strings.TrimSpace(out) != initiativeJSON(1) {
		t.Errorf("reopen --json: %d %q", code, out)
	}
}

func TestInitWriteFailuresNameTheNextCommand(t *testing.T) {
	const notFound = `{"title":"Not Found","detail":"no initiative zz","next":"GET /api/v1/initiatives"}`
	forceNeeded := `{"detail":"cannot close initiative aa: 1 of its tasks are not closed","next":"Close those tasks first, or send {\"force\": true}."}`
	closed := `{"detail":"initiative aa is already closed","next":"Reopen it with POST /api/v1/initiatives/aa/reopen."}`
	open := `{"detail":"initiative aa is open","next":"Only a closed initiative can be reopened."}`
	cases := []struct {
		args   []string
		status int
		body   string
		code   int
		want   string
	}{
		{[]string{"edit", "zz", "--title", "T"}, 404, notFound, ExitNotFound, "no initiative zz → board init ls\n"},
		{[]string{"close", "zz"}, 404, notFound, ExitNotFound, "no initiative zz → board init ls\n"},
		{[]string{"reopen", "zz"}, 404, notFound, ExitNotFound, "no initiative zz → board init ls\n"},
		{[]string{"close", "aa"}, 409, forceNeeded, ExitConflict, "cannot close initiative aa: 1 of its tasks are not closed → board init close aa --force\n"},
		{[]string{"close", "aa", "--force"}, 409, closed, ExitConflict, "initiative aa is already closed → board init reopen aa\n"},
		{[]string{"close", "aa"}, 409, closed, ExitConflict, "initiative aa is already closed → board init reopen aa\n"},
		{[]string{"reopen", "aa"}, 409, open, ExitConflict, "initiative aa is open → board init show aa\n"},
		{[]string{"edit", "aa", "--title", "T"}, 400, `{"detail":"title is empty","next":"fix it"}`, ExitError, "title is empty → fix it\n"},
		{[]string{"edit", "aa", "--title", "T"}, 401, `{}`, ExitAuth, "server answered 401 → " + authNext + "\n"},
		{[]string{"close", "aa"}, 500, `{"detail":"boom"}`, ExitError, "boom\n"},
	}
	for _, c := range cases {
		f := answering(c.status, c.body)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), append([]string{"init"}, c.args...)...)
		srv.Close()
		if code != c.code || out != "" || errs != c.want {
			t.Errorf("%v on %d: got %d %q %q, want %d %q", c.args, c.status, code, out, errs, c.code, c.want)
		}
	}
}

func TestInitWriteBadAnswerIsAnError(t *testing.T) {
	for _, args := range [][]string{{"init", "close", "aa"}, {"init", "reopen", "aa"}, {"init", "edit", "aa", "--title", "T"}} {
		code, out, errs := runEnv(func() Env {
			f := answering(200, `{"slug":7}`)
			srv := httptest.NewServer(f)
			t.Cleanup(srv.Close)
			return f.env(srv)
		}(), args...)
		if code != ExitError || out != "" || !strings.Contains(errs, "cannot unmarshal") {
			t.Errorf("%v: %d %q %q", args, code, out, errs)
		}
	}
}

func TestInitWriteNeedsAKey(t *testing.T) {
	for _, args := range [][]string{{"init", "edit", "ab", "--title", "T"}, {"init", "close", "ab"}, {"init", "reopen", "ab"}} {
		code, out, errs := runEnv(envWith(map[string]string{"BOARD_URL": "http://127.0.0.1:1"}), args...)
		if code != ExitAuth || out != "" || !strings.HasPrefix(errs, "no BOARD_API_KEY") {
			t.Errorf("%v: %d %q %q", args, code, out, errs)
		}
	}
}

func TestInitLsSendsTheStatusAndWindow(t *testing.T) {
	f := &fakeBoard{total: 3}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "init", "ls", "--status", "closed", "--closed-within", "7", "--tag", "t")
	if code != ExitOK || errs != "" || strings.Count(out, "\n") != 3 {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	q := f.requests[0].URL.Query()
	if q.Get("status") != "closed" || q.Get("closed_within") != "7" || q.Get("tag") != "t" {
		t.Errorf("query %v", q)
	}
	for _, status := range []string{"open", "all"} {
		f.requests = nil
		if code, _, _ := runEnv(f.env(srv), "init", "ls", "--status", status); code != ExitOK || f.requests[0].URL.Query().Get("status") != status {
			t.Errorf("status %s: %d %v", status, code, f.requests[0].URL.Query())
		}
	}
	f.requests = nil
	runEnv(f.env(srv), "init", "ls")
	if q := f.requests[0].URL.Query(); q.Has("status") || q.Has("closed_within") {
		t.Errorf("a plain ls sends %v", q)
	}
}

func TestInitLsMoreLineKeepsTheFilters(t *testing.T) {
	f := &fakeBoard{total: 3}
	srv := httptest.NewServer(f)
	defer srv.Close()
	_, out, _ := runEnv(f.env(srv), "init", "ls", "--status", "all", "--closed-within", "30d", "--tag", "a", "--limit", "2")
	want := "+1 more → board init ls --tag a --status all --closed-within 30d --all\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("got %q, want the suffix %q", out, want)
	}
}

func TestLsFilter(t *testing.T) {
	cases := []struct {
		args []string
		ok   bool
	}{
		{nil, true}, {[]string{"--status", "open"}, true}, {[]string{"--status", "all", "--closed-within", "90d"}, true},
		{[]string{"--status", "closed", "--closed-within", "12"}, true}, {[]string{"--status", "x"}, false},
		{[]string{"--closed-within", "7"}, false}, {[]string{"--status", "closed", "--closed-within", "007"}, false},
	}
	for _, c := range cases {
		p, _ := parseArgs(c.args, initLsFlags)
		if _, ok := lsFilter(p); ok != c.ok {
			t.Errorf("%v: ok %v, want %v", c.args, ok, c.ok)
		}
	}
}

func TestInitWriteCommandsAreInTheTree(t *testing.T) {
	tree := Commands()
	for cmd, flags := range map[string][]string{
		"init edit":   {"--clear-end", "--clear-start", "--description", "--end", "--json", "--no-tags", "--start", "--tag", "--title"},
		"init close":  {"--force", "--json"},
		"init reopen": {"--json"},
	} {
		if !reflect.DeepEqual(tree[cmd], flags) {
			t.Errorf("%s: flags %v, want %v", cmd, tree[cmd], flags)
		}
	}
	for _, f := range []string{"--status", "--closed-within"} {
		found := false
		for _, g := range tree["init ls"] {
			found = found || g == f
		}
		if !found {
			t.Errorf("init ls lacks %s", f)
		}
	}
}
