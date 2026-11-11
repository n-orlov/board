package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeBoard is a stand-in server that records the requests it gets.
type fakeBoard struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
	total    int // initiatives the list holds
	handler  func(w http.ResponseWriter, r *http.Request)
}

func initiativeJSON(i int) string {
	return fmt.Sprintf(`{"id":"01ID%d","ref":"slug-%02d","slug":"slug-%02d","title":"Title %d","description":"","status":"open","tags":["t"],"created_by":"a","created_at":"2026-01-01T00:00:00Z"}`, i, i, i, i)
}

func (f *fakeBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r.Body)
	f.requests = append(f.requests, r)
	f.bodies = append(f.bodies, buf.String())
	f.mu.Unlock()
	if f.handler != nil {
		f.handler(w, r)
		return
	}
	f.list(w, r)
}

// list serves GET /api/v1/initiatives in pages of at most 7 so a list spans pages.
func (f *fakeBoard) list(w http.ResponseWriter, r *http.Request) {
	after := 0
	if a := r.URL.Query().Get("after"); a != "" {
		_, _ = fmt.Sscanf(a, "slug-%d", &after)
	}
	var items []string
	for i := after + 1; i <= f.total && len(items) < 7; i++ {
		items = append(items, initiativeJSON(i))
	}
	next := ""
	if after+len(items) < f.total {
		next = fmt.Sprintf(`,"next_after":"slug-%02d"`, after+len(items))
	}
	_, _ = fmt.Fprintf(w, `{"items":[%s]%s}`, strings.Join(items, ","), next)
}

func (f *fakeBoard) env(srv *httptest.Server) Env {
	return envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_abcdefgh" + strings.Repeat("0", 44), "BOARD_ACTOR": "claude:test", "BOARD_WAY_BACK": "wb"})
}

func runEnv(env Env, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, env)
	return code, out.String(), errb.String()
}

func TestInitAddCreatesAndConfirms(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(initiativeJSON(1)))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "init", "add", "slug-01", "Title 1", "--tag", "work", "--tag=lab")
	if code != ExitOK || out != "ok initiative slug-01 created\n" || errs != "" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	var body struct {
		Slug, Title string
		Tags        []string
	}
	_ = json.Unmarshal([]byte(f.bodies[0]), &body)
	if f.requests[0].Method != "POST" || body.Slug != "slug-01" || body.Title != "Title 1" || strings.Join(body.Tags, ",") != "work,lab" {
		t.Fatalf("request %s %s", f.requests[0].Method, f.bodies[0])
	}
	if f.requests[0].Header.Get("Board-Actor") != "claude:test" {
		t.Fatalf("no actor: %v", f.requests[0].Header)
	}
}

func TestInitAddWithoutTagsSendsNone(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(initiativeJSON(1)))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "init", "add", "slug-01", "T", "--json")
	if code != ExitOK || strings.TrimSpace(out) != initiativeJSON(1) || strings.Contains(f.bodies[0], "tags") {
		t.Fatalf("got %d %q body %s", code, out, f.bodies[0])
	}
}

func TestInitAddRefusals(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"title":"Conflict","detail":"initiative \"x\" already exists","next":"read it"}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "init", "add", "xx", "T")
	if code != ExitError || out != "" || errs != "initiative \"x\" already exists → board init show xx\n" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`[1]`))
	}
	if code, _, errs := runEnv(f.env(srv), "init", "add", "xx", "T"); code != ExitError || !strings.Contains(errs, "cannot unmarshal") {
		t.Fatalf("bad body: %d %q", code, errs)
	}
}

// TestInitCommandsKeepPhaseZeroExitCodes holds every init subcommand to the
// Phase 0 exit-code set (0, 1, 2, 4, 5) for the error statuses a server can
// answer with, a taken slug (409) included, with one actionable stderr line.
func TestInitCommandsKeepPhaseZeroExitCodes(t *testing.T) {
	allowed := map[int]bool{ExitOK: true, ExitError: true, ExitUsage: true, ExitAuth: true, ExitNotFound: true}
	commands := [][]string{{"init", "add", "xx", "T"}, {"init", "ls"}, {"init", "ls", "--all"}, {"init", "show", "xx"}, {"init", "show", "xx", "--json"}}
	for _, status := range []int{400, 401, 403, 404, 409, 410, 422, 429, 500, 503} {
		for _, args := range commands {
			f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"title":"Err","detail":"it failed","next":"read it"}`))
			}}
			srv := httptest.NewServer(f)
			code, out, errs := runEnv(f.env(srv), args...)
			srv.Close()
			if !allowed[code] || code == ExitOK || out != "" || strings.Count(errs, "\n") != 1 || !strings.HasSuffix(errs, "\n") {
				t.Errorf("%v on %d: exit %d out %q err %q", args, status, code, out, errs)
			}
		}
	}
}

// TestInitAddTakenSlugNamesTheSlugItWasGiven checks the 409 error stays
// actionable for any slug: it points at 'board init show <that slug>'.
func TestInitAddTakenSlugNamesTheSlugItWasGiven(t *testing.T) {
	for _, slug := range []string{"a", "phone-app", "x9"} {
		f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"title":"Conflict","detail":"initiative taken","next":"read it"}`))
		}}
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), "init", "add", slug, "T")
		srv.Close()
		if code != ExitError || out != "" || errs != "initiative taken → board init show "+slug+"\n" {
			t.Errorf("%s: got %d %q %q", slug, code, out, errs)
		}
	}
}

func TestInitLsPagesAndCaps(t *testing.T) {
	f := &fakeBoard{total: 25}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "init", "ls")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != ExitOK || len(lines) != 21 || lines[0] != "slug-01  open  Title 1  [t]" || lines[20] != "+5 more → board init ls --all" {
		t.Fatalf("got %d %d lines, last %q", code, len(lines), lines[len(lines)-1])
	}
	if len(f.requests) != 4 {
		t.Fatalf("%d requests, want 4 pages", len(f.requests))
	}
	if q := f.requests[1].URL.Query(); q.Get("after") != "slug-07" || q.Get("limit") != "1000" {
		t.Fatalf("second page query %v", q)
	}
	_, out, _ = runEnv(f.env(srv), "init", "ls", "--limit", "2", "--tag", "t", "--tag", "u")
	if out != "slug-01  open  Title 1  [t]\nslug-02  open  Title 2  [t]\n+23 more → board init ls --tag t --tag u --all\n" {
		t.Fatalf("limit 2: %q", out)
	}
	if got := f.requests[len(f.requests)-1].URL.Query()["tag"]; strings.Join(got, ",") != "t,u" {
		t.Fatalf("tags sent %v", got)
	}
	_, out, _ = runEnv(f.env(srv), "init", "ls", "--all")
	if strings.Count(out, "\n") != 25 || strings.Contains(out, "more") {
		t.Fatalf("--all: %d lines", strings.Count(out, "\n"))
	}
}

func TestInitLsJSONAndEmpty(t *testing.T) {
	f := &fakeBoard{total: 3}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "init", "ls", "--json", "--limit", "2")
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); code != ExitOK || err != nil || len(items) != 2 || items[0]["id"] != "01ID1" {
		t.Fatalf("got %d %q %v", code, out, err)
	}
	f.total = 0
	if code, out, errs := runEnv(f.env(srv), "init", "ls"); code != ExitOK || out != "" || errs != "" {
		t.Fatalf("empty: %d %q %q", code, out, errs)
	}
}

func TestInitLsFailures(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[42]}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, _, errs := runEnv(f.env(srv), "init", "ls"); code != ExitError || !strings.Contains(errs, "cannot unmarshal") {
		t.Fatalf("bad item: %d %q", code, errs)
	}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"detail":"key revoked","next":"ask for a new key"}`))
	}
	if code, _, errs := runEnv(f.env(srv), "init", "ls"); code != ExitAuth || errs != "key revoked → "+authNext+"\n" {
		t.Fatalf("401: %d %q", code, errs)
	}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"next_after":"loop"}`))
	}
	f.requests = nil
	if code, _, _ := runEnv(f.env(srv), "init", "ls"); code != ExitOK || len(f.requests) != 2 {
		t.Fatalf("a repeated cursor must end the walk: %d, %d requests", code, len(f.requests))
	}
	if code, _, _ := runEnv(f.env(srv), "init", "ls", "--json"); code != ExitOK {
		t.Fatalf("json on an empty list: %d", code)
	}
}

func TestInitShow(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/initiatives/missing" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"detail":"initiative missing not found","next":"List initiatives with GET /api/v1/initiatives."}`))
			return
		}
		_, _ = w.Write([]byte(`{"slug":"phone","title":"Phone","description":"The body.","status":"closed","tags":["a","b"],"target_start":"2026-01-01","target_end":"2026-02-01","created_by":"x","created_at":"2000-01-01T00:00:00Z","closed_at":"2000-01-02T00:00:00Z","closed_by":"y"}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "init", "show", "phone")
	if code != ExitOK || !strings.HasPrefix(out, "phone  closed  Phone  [a,b]\ntargets: 2026-01-01 → 2026-02-01\ncreated by x ") || !strings.HasSuffix(out, "d ago\nThe body.\n") || !strings.Contains(out, "\nclosed by y ") {
		t.Fatalf("got %d %q", code, out)
	}
	if code, out, _ := runEnv(f.env(srv), "init", "show", "phone", "--json"); code != ExitOK || !strings.HasPrefix(out, `{"slug":"phone"`) {
		t.Fatalf("json: %d %q", code, out)
	}
	if code, _, errs := runEnv(f.env(srv), "init", "show", "missing"); code != ExitNotFound || errs != "initiative missing not found → board init ls\n" {
		t.Fatalf("404: %d %q", code, errs)
	}
	if got := f.requests[0].URL.Path; got != "/api/v1/initiatives/phone" {
		t.Fatalf("path %q", got)
	}
}

func TestInitShowMinimal(t *testing.T) {
	out := initiativeDetail(initiative{Slug: "s", Title: "T", Status: "open", CreatedBy: "x", CreatedAt: "2026-01-01T00:00:00Z"}, nowFn())
	if out != "s  open  T\ncreated by x "+ago(nowFn(), "2026-01-01T00:00:00Z")+"\n" {
		t.Fatalf("got %q", out)
	}
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[]`)) }}
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, _, errs := runEnv(f.env(srv), "init", "show", "s"); code != ExitError || errs == "" {
		t.Fatalf("bad body: %d %q", code, errs)
	}
}

func TestInitUsageErrors(t *testing.T) {
	env := envWith(map[string]string{"BOARD_URL": "http://127.0.0.1:1", "BOARD_API_KEY": "brd_k"})
	for _, args := range [][]string{
		{"init"}, {"init", "bogus"},
		{"init", "add"}, {"init", "add", "only-slug"}, {"init", "add", "a", "b", "c"}, {"init", "add", "a", "b", "--nope"},
		{"init", "ls", "extra"}, {"init", "ls", "--limit", "0"}, {"init", "ls", "--limit", "x"}, {"init", "ls", "--all", "--limit", "3"}, {"init", "ls", "--tag"},
		{"init", "show"}, {"init", "show", "a", "b"},
	} {
		code, out, errs := runEnv(env, args...)
		if code != ExitUsage || out != "" || errs != InitUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestClientCommandsNeedAKeyAndAServer(t *testing.T) {
	for _, args := range [][]string{{"init", "add", "ab", "T"}, {"init", "ls"}, {"init", "show", "ab"}, {"whoami"}} {
		code, out, errs := runEnv(envWith(map[string]string{"BOARD_URL": "http://127.0.0.1:1"}), args...)
		if code != ExitAuth || out != "" || errs != "no BOARD_API_KEY → export BOARD_API_KEY (this environment's key)\n" {
			t.Errorf("%v: %d %q %q", args, code, out, errs)
		}
		code, _, errs = runEnv(envWith(map[string]string{"BOARD_API_KEY": "brd_k"}), args...)
		if code != ExitError || !strings.HasPrefix(errs, "no BOARD_URL → ") {
			t.Errorf("%v: %d %q", args, code, errs)
		}
		code, _, errs = runEnv(envWith(map[string]string{"BOARD_API_KEY": "brd_k", "BOARD_URL": "http://127.0.0.1:1"}), args...)
		if code != ExitError || !strings.HasPrefix(errs, "server unreachable at http://127.0.0.1:1 → ") {
			t.Errorf("%v: %d %q", args, code, errs)
		}
	}
}

func TestNoKeyFlagIsRegistered(t *testing.T) {
	tables := map[string][]flagSpec{"init add": initAddFlags, "init ls": initLsFlags, "init show": initShowFlags, "whoami": whoamiFlags}
	for cmd, specs := range tables {
		for _, s := range specs {
			name := strings.ToLower(s.Name)
			for _, banned := range []string{"key", "token", "secret", "password", "bearer", "auth"} {
				if strings.Contains(name, banned) {
					t.Errorf("%s registers %s: the API key is environment-only", cmd, s.Name)
				}
			}
		}
	}
}

func TestKeyFlagsAreUsageErrors(t *testing.T) {
	env := envWith(map[string]string{"BOARD_URL": "http://127.0.0.1:1", "BOARD_API_KEY": "brd_k"})
	for _, args := range [][]string{
		{"init", "add", "ab", "T", "--api-key", "brd_x"}, {"init", "ls", "--api-key=brd_x"}, {"init", "show", "ab", "--api-key", "x"},
		{"whoami", "--api-key", "brd_x"}, {"whoami", "--key=x"},
	} {
		code, out, errs := runEnv(env, args...)
		if code != ExitUsage || out != "" || strings.Contains(errs, "brd_x") || strings.Count(errs, "\n") != 1 {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestListCap(t *testing.T) {
	cases := []struct {
		args []string
		want int
		ok   bool
	}{
		{nil, DefaultListCap, true}, {[]string{"--limit", "3"}, 3, true}, {[]string{"--all"}, 0, true},
		{[]string{"--all", "--limit", "3"}, 0, false}, {[]string{"--limit", "0"}, 0, false}, {[]string{"--limit", "x"}, 0, false},
	}
	for _, c := range cases {
		p, _ := parseArgs(c.args, initLsFlags)
		if n, ok := listCap(p); ok != c.ok || (ok && n != c.want) {
			t.Errorf("%v: %d %v", c.args, n, ok)
		}
	}
}

func TestPrintJSONReportsAnUnencodableValue(t *testing.T) {
	var out, errb bytes.Buffer
	if code := printJSON(&out, &errb, []json.RawMessage{json.RawMessage("{")}); code != ExitError || errb.Len() == 0 || out.Len() != 0 {
		t.Fatalf("got %d %q %q", code, out.String(), errb.String())
	}
}

func TestInitiativeLineAppendsTheAttentionCountOnlyWhenNotZero(t *testing.T) {
	cases := []struct {
		in   initiative
		want string
	}{
		{initiative{Slug: "aa", Status: "open", Title: "T"}, "aa  open  T"},
		{initiative{Slug: "aa", Status: "open", Title: "T", Attention: 1}, "aa  open  T !1"},
		{initiative{Slug: "aa", Status: "open", Title: "T", Tags: []string{"x", "y"}, Attention: 12}, "aa  open  T  [x,y] !12"},
		{initiative{Slug: "aa", Status: "open", Title: "T", Tags: []string{"x"}}, "aa  open  T  [x]"},
	}
	for _, c := range cases {
		if got := initiativeLine(c.in); got != c.want {
			t.Errorf("initiativeLine(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
