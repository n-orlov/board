package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/idempotency"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/server"
	"github.com/n-orlov/village-hall/internal/store"
)

// statusOf is the status of the apiError err is, or 0.
func statusOf(err error) int {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func noRetryPause(t *testing.T) {
	t.Helper()
	old := retryPause
	retryPause = 0
	t.Cleanup(func() { retryPause = old })
}

// scripted answers each request with the next status of the script (the last
// one repeats) and records the Idempotency-Key header of every request.
type scripted struct {
	mu       sync.Mutex
	statuses []int
	keys     []string
	present  []bool
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.keys)
	s.keys = append(s.keys, r.Header.Get("Idempotency-Key"))
	_, ok := r.Header["Idempotency-Key"]
	s.present = append(s.present, ok)
	status := s.statuses[min(n, len(s.statuses)-1)]
	s.mu.Unlock()
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"detail":"answer ` + http.StatusText(status) + `"}`))
}

func scriptedClient(t *testing.T, statuses ...int) (*client, *scripted) {
	t.Helper()
	noRetryPause(t)
	s := &scripted{statuses: statuses}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	c, err := newClient(envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "a"}))
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func TestAWriteRetriesOnceOnA502A503Or504WithTheSameKey(t *testing.T) {
	for _, status := range []int{502, 503, 504} {
		c, s := scriptedClient(t, status, 200)
		if err := c.do(t.Context(), "POST", "/w", nil, map[string]int{"a": 1}, nil); err != nil {
			t.Fatalf("%d then 200: %v", status, err)
		}
		if len(s.keys) != 2 || s.keys[0] == "" || s.keys[0] != s.keys[1] {
			t.Errorf("%d: keys %q, want two requests with one non-empty key", status, s.keys)
		}
	}
}

func TestAWriteRetriesExactlyOnceAndThenReportsTheFailure(t *testing.T) {
	for _, status := range []int{502, 503, 504} {
		c, s := scriptedClient(t, status)
		err := c.do(t.Context(), "POST", "/w", nil, nil, nil)
		if statusOf(err) != status {
			t.Fatalf("%d: got %#v, want the apiError of the second answer", status, err)
		}
		if len(s.keys) != 2 || s.keys[0] != s.keys[1] {
			t.Errorf("%d: %d requests with keys %q, want exactly two with one key", status, len(s.keys), s.keys)
		}
	}
}

func TestAWriteIsNotRetriedOnAnyOtherFailure(t *testing.T) {
	for _, status := range []int{400, 401, 404, 409, 410, 422, 500, 501} {
		c, s := scriptedClient(t, status, 200)
		err := c.do(t.Context(), "POST", "/w", nil, nil, nil)
		if statusOf(err) != status {
			t.Fatalf("%d: got %#v", status, err)
		}
		if len(s.keys) != 1 {
			t.Errorf("%d: %d requests, want exactly one", status, len(s.keys))
		}
	}
}

func TestASuccessIsNotRetried(t *testing.T) {
	c, s := scriptedClient(t, 201, 201)
	if err := c.do(t.Context(), "POST", "/w", nil, nil, nil); err != nil || len(s.keys) != 1 {
		t.Fatalf("err %v, %d requests", err, len(s.keys))
	}
}

func TestEveryWriteMethodCarriesAFreshPrintableKeyAndAReadCarriesNone(t *testing.T) {
	printable := regexp.MustCompile(`^[\x21-\x7e]{1,255}$`)
	seen := map[string]bool{}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for range 2 {
			c, s := scriptedClient(t, 200)
			if err := c.do(t.Context(), method, "/w", nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			k := s.keys[0]
			if !printable.MatchString(k) || seen[k] {
				t.Errorf("%s: key %q is not a fresh 1-255 printable ASCII key", method, k)
			}
			seen[k] = true
		}
	}
	c, s := scriptedClient(t, 503, 200)
	err := c.do(t.Context(), "GET", "/r", nil, nil, nil)
	if statusOf(err) != 503 {
		t.Fatalf("a read must not be retried, got %#v", err)
	}
	if len(s.keys) != 1 || s.present[0] {
		t.Errorf("a read sent %d requests, key header present: %v", len(s.keys), s.present)
	}
}

func TestAWriteRetriesOnceOnAConnectionErrorAndThenReportsIt(t *testing.T) {
	noRetryPause(t)
	var calls atomic.Int32
	keys := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		keys <- r.Header.Get("Idempotency-Key")
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()
	c, _ := newClient(envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "a"}))
	err := c.do(t.Context(), "POST", "/w", nil, nil, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "server unreachable at ") {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d requests, want exactly two", calls.Load())
	}
	if a, b := <-keys, <-keys; a == "" || a != b {
		t.Errorf("keys %q and %q, want one non-empty key", a, b)
	}
}

func TestACancelledWriteIsNotRetried(t *testing.T) {
	noRetryPause(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	c, _ := newClient(envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "a"}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.do(ctx, "POST", "/w", nil, nil, nil); err == nil {
		t.Fatal("a cancelled write must fail")
	}
	if calls.Load() != 0 {
		t.Fatalf("%d requests reached the server", calls.Load())
	}
}

func TestTheRetryKeepsTheBodyOfTheWrite(t *testing.T) {
	noRetryPause(t)
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(503)
		}
	}))
	defer srv.Close()
	c, _ := newClient(envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "a"}))
	if err := c.do(t.Context(), "POST", "/w", nil, map[string]string{"k": "v"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != `{"k":"v"}` || bodies[1] != bodies[0] {
		t.Fatalf("bodies %q", bodies)
	}
}

// dropFirstResponse is a proxy to backend that forwards the first request,
// waits for the backend's answer and then closes the connection without
// relaying it; every later request is forwarded as it is.
func dropFirstResponse(t *testing.T, backend string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	target, err := url.Parse(backend)
	if err != nil {
		t.Fatal(err)
	}
	var seen atomic.Int32
	rp := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen.Add(1) > 1 {
			rp.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		rp.ServeHTTP(rec, r)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func countRows(t *testing.T, st *store.Store, query string) int {
	t.Helper()
	var n int
	if err := st.Reader().QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestACommandThroughAProxyThatDropsTheFirstResponseHasOneEffectAndExitsZero(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context, string) (string, error) { return "key:x", nil }
	backend := httptest.NewServer(server.NewHandler(func(context.Context) error { return nil }, verify, nil, (*auth.Keys)(nil),
		server.WithInitiatives(&initiatives.Store{Store: st}), server.WithIdempotency(&idempotency.Store{Store: st})))
	defer backend.Close()
	proxy, seen := dropFirstResponse(t, backend.URL)
	env := envWith(map[string]string{"BOARD_URL": proxy.URL, "BOARD_API_KEY": "brd_k", "BOARD_ACTOR": "claude@host"})
	var out, errb bytes.Buffer
	if code := Run([]string{"init", "add", "dropped", "A dropped answer"}, &out, &errb, env); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if seen.Load() != 2 {
		t.Fatalf("the proxy saw %d requests, want the write and its one retry", seen.Load())
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM events WHERE type = 'initiative_created'`); n != 1 {
		t.Fatalf("%d initiative_created events, want exactly one", n)
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM initiatives WHERE slug = 'dropped'`); n != 1 {
		t.Fatalf("%d initiatives, want exactly one", n)
	}
}
