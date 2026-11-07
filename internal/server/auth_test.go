package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/auth"
)

// countingVerifier records how often the store would be consulted.
type countingVerifier struct {
	calls int
	err   error
}

func (c *countingVerifier) verify(context.Context, string) (string, error) {
	c.calls++
	return "k", c.err
}

func request(path string, headers map[string]string) *httptest.ResponseRecorder {
	return do(&countingVerifier{}, path, headers)
}

func do(c *countingVerifier, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	NewHandler(ok, c.verify, nil, nil).ServeHTTP(rec, r)
	return rec
}

func problem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	if int(doc["status"].(float64)) != rec.Code {
		t.Fatalf("problem status %v, response %d", doc["status"], rec.Code)
	}
	return doc
}

func TestNoCredentialIs401WithAProblemAndNeverTouchesTheStore(t *testing.T) {
	noScheme := map[string]string{"Authorization": "brd_x"}
	basic := map[string]string{"Authorization": "Basic Zm9vOmJhcg=="}
	empty := map[string]string{"Authorization": "Bearer "}
	for _, path := range []string{"/readyz", "/", "/api/v1/tasks", "/no-such"} {
		for _, h := range []map[string]string{nil, noScheme, basic, empty, {ActorHeader: "a"}} {
			c := &countingVerifier{}
			rec := do(c, path, h)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("GET %s headers %v = %d, want 401", path, h, rec.Code)
				continue
			}
			problem(t, rec)
			if c.calls != 0 {
				t.Errorf("GET %s headers %v consulted the store %d times", path, h, c.calls)
			}
		}
	}
}

func TestHealthzNeedsNoCredentialButOtherMethodsDo(t *testing.T) {
	if rec := request("/healthz", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", rec.Code)
	}
	rec := httptest.NewRecorder()
	NewHandler(ok, (&countingVerifier{}).verify, nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /healthz = %d, want 401", rec.Code)
	}
}

func TestRefusedKeysAre401WithTheReasonInTheDetail(t *testing.T) {
	cases := map[string]error{
		"key unknown": auth.ErrKeyUnknown,
		"key revoked": auth.ErrKeyRevoked,
		"key expired": auth.ErrKeyExpired,
	}
	h := map[string]string{"Authorization": "Bearer brd_x", ActorHeader: "a"}
	for want, err := range cases {
		rec := do(&countingVerifier{err: err}, "/readyz", h)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", want, rec.Code)
		}
		if got := problem(t, rec)["detail"]; got != want {
			t.Errorf("detail = %v, want %q", got, want)
		}
	}
}

func TestValidKeyWithoutActorIs400(t *testing.T) {
	for _, actor := range []map[string]string{{}, {ActorHeader: "  "}} {
		actor["Authorization"] = "bearer brd_x"
		rec := request("/readyz", actor)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", rec.Code)
		}
		if d := problem(t, rec)["detail"].(string); !strings.Contains(d, ActorHeader) {
			t.Errorf("detail %q does not name %s", d, ActorHeader)
		}
	}
}

func TestStoreFailureIs503NeverA2xx(t *testing.T) {
	h := map[string]string{"Authorization": "Bearer brd_x", ActorHeader: "a"}
	rec := do(&countingVerifier{err: errors.New("connection refused")}, "/readyz", h)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	problem(t, rec)
}

func TestValidKeyAndActorReachTheRoute(t *testing.T) {
	h := map[string]string{"Authorization": "Bearer brd_x", ActorHeader: "a"}
	if rec := request("/readyz", h); rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
}

func (s *keyServer) lastUsed() bool {
	s.t.Helper()
	var n int
	if err := s.st.Reader().QueryRowContext(context.Background(), `SELECT count(last_used_at) FROM api_keys WHERE name = 'caller'`).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n == 1
}

func TestAKeyIsMarkedUsedOnlyByARequestThatSucceeds(t *testing.T) {
	s := newKeyServer(t)
	refused := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/api/v1/leases", `{"task":"aa-9","intent":"x"}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/leases/L9/renew", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/leases/L9/end", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/leases/L9/ack", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/leases", `{`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/tasks/aa-9", "", http.StatusNotFound},
	}
	for _, tc := range refused {
		if rec := s.send(tc.method, tc.path, tc.body, "a@b"); rec.Code != tc.status {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.status)
		}
		if s.lastUsed() {
			t.Fatalf("%s %s, answered %d, marked the key used", tc.method, tc.path, tc.status)
		}
	}
	if rec := s.send(http.MethodGet, "/api/v1/leases", "", ""); rec.Code != http.StatusBadRequest || s.lastUsed() {
		t.Fatalf("a request with no actor = %d, used %v; want 400, unused", rec.Code, s.lastUsed())
	}
	if rec := s.send(http.MethodGet, "/api/v1/leases", "", "a@b"); rec.Code != http.StatusOK || !s.lastUsed() {
		t.Fatalf("a successful read = %d, used %v; want 200, used", rec.Code, s.lastUsed())
	}
}
