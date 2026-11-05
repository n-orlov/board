package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withHost(t *testing.T, host string, hostErr error, ppid int, cwd string, cwdErr error) {
	t.Helper()
	h, p, w := hostnameFn, ppidFn, getwdFn
	t.Cleanup(func() { hostnameFn, ppidFn, getwdFn = h, p, w })
	hostnameFn = func() (string, error) { return host, hostErr }
	ppidFn = func() int { return ppid }
	getwdFn = func() (string, error) { return cwd, cwdErr }
}

func TestIdentityUsesTheEnvironment(t *testing.T) {
	id := resolveIdentity(envWith(map[string]string{"BOARD_ACTOR": "ralph:r1", "BOARD_WAY_BACK": "ralphctl status r1"}))
	if id.Actor != "ralph:r1" || id.WayBack != "ralphctl status r1" {
		t.Fatalf("got %+v", id)
	}
	if id = resolveIdentity(envWith(map[string]string{"BOARD_ACTOR": "a"})); id.WayBack != "" {
		t.Fatalf("a set actor with no way back must send none, got %+v", id)
	}
}

func TestIdentityFallsBackToTheHostAndParentPID(t *testing.T) {
	withHost(t, "box", nil, 4242, "/work", nil)
	id := resolveIdentity(envWith(nil))
	if id.Actor != "agent:box:4242" || id.WayBack != "pid 4242 on box, cwd /work" {
		t.Fatalf("got %+v", id)
	}
	id = resolveIdentity(envWith(map[string]string{"BOARD_WAY_BACK": "mine"}))
	if id.Actor != "agent:box:4242" || id.WayBack != "mine" {
		t.Fatalf("BOARD_WAY_BACK must win over the fallback way back, got %+v", id)
	}
}

func TestIdentityFallbackSurvivesHostFailures(t *testing.T) {
	withHost(t, "", errors.New("no host"), 7, "", errors.New("no cwd"))
	id := resolveIdentity(envWith(nil))
	if id.Actor != "agent:unknown:7" || id.WayBack != "pid 7 on unknown, cwd unknown" {
		t.Fatalf("got %+v", id)
	}
}

func TestIdentityFallbackNeverReadsToolVariables(t *testing.T) {
	withHost(t, "box", nil, 1, "/", nil)
	env := envWith(map[string]string{"RALPHD_RUN_ID": "r", "DECK_SESSION_NAME": "s", "CLAUDE_CODE_SESSION_ID": "c"})
	if id := resolveIdentity(env); id.Actor != "agent:box:1" {
		t.Fatalf("got %+v", id)
	}
}

func TestNewClientNeedsKeyAndURL(t *testing.T) {
	_, err := newClient(envWith(map[string]string{"BOARD_URL": "http://x"}))
	var ae *apiError
	if !errors.As(err, &ae) || ae.exitCode() != ExitAuth || err.Error() != "no BOARD_API_KEY → export BOARD_API_KEY (this environment's key)" {
		t.Fatalf("missing key: %v", err)
	}
	_, err = newClient(envWith(map[string]string{"BOARD_API_KEY": "brd_x"}))
	if err == nil || err.Error() != "no BOARD_URL → export BOARD_URL (the address of the board server)" {
		t.Fatalf("missing url: %v", err)
	}
	c, err := newClient(envWith(map[string]string{"BOARD_API_KEY": "brd_x", "BOARD_URL": "http://x///"}))
	if err != nil || c.base != "http://x" {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestAPIErrorExitCodes(t *testing.T) {
	for status, want := range map[int]int{401: ExitAuth, 404: ExitNotFound, 409: ExitConflict, 400: ExitError, 503: ExitError} {
		if got := (&apiError{Status: status}).exitCode(); got != want {
			t.Errorf("status %d: exit %d, want %d", status, got, want)
		}
	}
}

func TestAPIErrorText(t *testing.T) {
	if got := newAPIError(404, []byte(`{"title":"Not Found","detail":"initiative x not found","next":"board init ls"}`)).Error(); got != "initiative x not found → board init ls" {
		t.Fatalf("got %q", got)
	}
	if got := newAPIError(400, []byte(`{"title":"Bad Request"}`)).Error(); got != "Bad Request" {
		t.Fatalf("got %q", got)
	}
	if got := newAPIError(502, []byte(`<html>`)).Error(); got != "server answered 502" {
		t.Fatalf("got %q", got)
	}
}

func TestDoSendsTheIdentityAndTheKeyOnly(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := newClient(envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_secretkey", "BOARD_ACTOR": "a:b", "BOARD_WAY_BACK": "wb"}))
	var out map[string]any
	if err := c.do(t.Context(), "POST", "/p", map[string][]string{"q": {"1"}}, map[string]string{"k": "v"}, &out); err != nil {
		t.Fatal(err)
	}
	h := got.Header
	if h.Get("Authorization") != "Bearer brd_secretkey" || h.Get("Board-Actor") != "a:b" || h.Get("Board-Actor-Way-Back") != "wb" || h.Get("Content-Type") != "application/json" {
		t.Fatalf("headers %v", h)
	}
	if got.URL.RawQuery != "q=1" || strings.Contains(got.URL.String(), "brd_") {
		t.Fatalf("url %v", got.URL)
	}
}

func TestDoOmitsEmptyWayBackAndBody(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header }))
	defer srv.Close()
	c, _ := newClient(envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "a"}))
	if err := c.do(t.Context(), "GET", "/p", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["Board-Actor-Way-Back"]; ok || got.Get("Content-Type") != "" {
		t.Fatalf("headers %v", got)
	}
}

func TestDoFailureModes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notjson":
			_, _ = w.Write([]byte(`nope`))
		case "/cut":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("abc"))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		default:
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"detail":"taken","next":"read it"}`))
		}
	}))
	env := func(url string) Env {
		return envWith(map[string]string{"BOARD_URL": url, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "a"})
	}
	c, _ := newClient(env(srv.URL))
	var out map[string]any
	for path, want := range map[string]string{"/notjson": "not the expected JSON", "/cut": "reading the server's answer", "/x": "taken → read it"} {
		if err := c.do(t.Context(), "GET", path, nil, nil, &out); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", path, err, want)
		}
	}
	if err := c.do(t.Context(), "POST", "/x", nil, make(chan int), nil); err == nil {
		t.Error("an unmarshalable body must fail")
	}
	srv.Close()
	if err := c.do(t.Context(), "GET", "/x", nil, nil, nil); err == nil || !strings.HasPrefix(err.Error(), "server unreachable at "+srv.URL+" → ") {
		t.Errorf("closed server: %v", err)
	}
	bad, _ := newClient(env("http://[::1"))
	if err := bad.do(t.Context(), "GET", "/x", nil, nil, nil); err == nil || !strings.HasPrefix(err.Error(), "bad BOARD_URL") {
		t.Errorf("bad url: %v", err)
	}
}

func TestParseArgs(t *testing.T) {
	specs := []flagSpec{{"--tag", true}, {"--all", false}}
	good := []struct {
		args []string
		pos  int
		tags []string
		all  bool
	}{
		{[]string{"a", "--tag", "x", "b", "--tag=y", "--all"}, 2, []string{"x", "y"}, true},
		{[]string{"--", "--tag", "-x"}, 2, nil, false},
		{nil, 0, nil, false},
	}
	for _, g := range good {
		p, ok := parseArgs(g.args, specs)
		if !ok || len(p.pos) != g.pos || strings.Join(p.values["--tag"], ",") != strings.Join(g.tags, ",") || p.has("--all") != g.all {
			t.Errorf("%v: %+v ok=%v", g.args, p, ok)
		}
	}
	for _, bad := range [][]string{{"--nope"}, {"--tag"}, {"--all=1"}, {"-x"}} {
		if _, ok := parseArgs(bad, specs); ok {
			t.Errorf("%v must be refused", bad)
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	for stamp, want := range map[string]string{
		"2026-01-10T11:59:40Z": "just now",
		"2026-01-10T11:55:00Z": "5m ago",
		"2026-01-10T09:00:00Z": "3h ago",
		"2026-01-07T12:00:00Z": "3d ago",
		"garbage":              "garbage",
	} {
		if got := ago(now, stamp); got != want {
			t.Errorf("%s: %q, want %q", stamp, got, want)
		}
	}
}
