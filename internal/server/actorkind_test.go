package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/auth"
)

// kindServer serves key and session writes over one database, so a test can
// reuse the operator's handle through both and in either order.
type kindServer struct {
	t     *testing.T
	l     *Login
	h     http.Handler
	token string
	n     int
}

func newKindServer(t *testing.T) *kindServer {
	t.Helper()
	l := newLogin(t, &fakeIdentity{})
	keys := &auth.Keys{Store: l.Sessions.Store}
	token, _, err := keys.Create(context.Background(), "caller", "admin-cli", 0)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(ok, (&auth.Verifier{Store: l.Sessions.Store}).Verify, l, keys)
	return &kindServer{t: t, l: l, h: h, token: token}
}

// keyWrite creates a key by API key, acting as handle.
func (s *kindServer) keyWrite(handle string) {
	s.t.Helper()
	s.n++
	r := httptest.NewRequest(http.MethodPost, "/api/v1/keys", strings.NewReader(fmt.Sprintf(`{"name":"k%d"}`, s.n)))
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set(ActorHeader, handle)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusCreated {
		s.t.Fatalf("key write as %q = %d %s", handle, rec.Code, rec.Body)
	}
}

// sessionWrite creates a key through a browser session, which acts as the operator.
func (s *kindServer) sessionWrite() { s.sessionWriteAs("alice@example.com") }

// sessionWriteAs is sessionWrite for a session of email, and returns the
// actor that GET /auth/session reports for it.
func (s *kindServer) sessionWriteAs(email string) string {
	s.t.Helper()
	s.n++
	id, sess, err := s.l.Sessions.Create(context.Background(), email)
	if err != nil {
		s.t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/keys", strings.NewReader(fmt.Sprintf(`{"name":"s%d"}`, s.n)))
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: id, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	r.Header.Set(CSRFHeader, sess.CSRFToken)
	r.Header.Set("Origin", publicURL)
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusCreated {
		s.t.Fatalf("session write = %d %s", rec.Code, rec.Body)
	}
	who := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	who.AddCookie(&http.Cookie{Name: SessionCookie, Value: id, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	rec = httptest.NewRecorder()
	s.h.ServeHTTP(rec, who)
	var doc struct{ Actor string }
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || rec.Code != http.StatusOK {
		s.t.Fatalf("GET /auth/session = %d %s: %v", rec.Code, rec.Body, err)
	}
	return doc.Actor
}

func (s *kindServer) kinds() map[string]string {
	s.t.Helper()
	rows, err := s.l.Sessions.Store.Reader().QueryContext(context.Background(), `SELECT handle, kind FROM actors`)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var h, k string
		if err := rows.Scan(&h, &k); err != nil {
			s.t.Fatal(err)
		}
		out[h] = k
	}
	return out
}

func (s *kindServer) principals(handle string) []string {
	s.t.Helper()
	rows, err := s.l.Sessions.Store.Reader().QueryContext(context.Background(),
		`SELECT e.principal FROM events e JOIN actors a ON a.id = e.actor_id WHERE a.handle = ? ORDER BY e.n`, handle)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			s.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestOperatorHandleUsedByAKeyThenASessionEndsUpOperator(t *testing.T) {
	t.Parallel()
	s := newKindServer(t)
	s.keyWrite("operator")
	s.sessionWrite()
	if got := s.kinds()["operator"]; got != "operator" {
		t.Errorf("kind = %q", got)
	}
	if got := strings.Join(s.principals("operator"), ","); got != "key:caller,oidc:alice@example.com" {
		t.Errorf("principals = %q", got)
	}
}

func TestOperatorHandleUsedByASessionThenAKeyStaysOperator(t *testing.T) {
	t.Parallel()
	s := newKindServer(t)
	s.sessionWrite()
	s.keyWrite("operator")
	if got := s.kinds()["operator"]; got != "operator" {
		t.Errorf("kind = %q", got)
	}
	if got := strings.Join(s.principals("operator"), ","); got != "oidc:alice@example.com,key:caller" {
		t.Errorf("principals = %q", got)
	}
}

func TestKeyWritesUnderOtherHandlesStayAgentsWhateverTheOrder(t *testing.T) {
	t.Parallel()
	s := newKindServer(t)
	s.keyWrite("ralph:run-1")
	s.sessionWrite()
	s.keyWrite("operator")
	s.keyWrite("ralph:run-1")
	s.sessionWrite()
	k := s.kinds()
	if k["ralph:run-1"] != "agent" || k["operator"] != "operator" || len(k) != 2 {
		t.Errorf("kinds = %v", k)
	}
}

func TestEachMappedEmailActsAsItsOwnOperatorAndAnUnmappedOneAsTheDefault(t *testing.T) {
	t.Parallel()
	s := newKindServer(t)
	s.l.Operators = map[string]string{"alice@example.com": "alice-op", "bob@example.com": "shared-op", "carol@example.com": "shared-op"}
	// An ordered slice, not a map: the principals assertion below depends on sign-in order.
	for _, c := range []struct{ email, want string }{
		{"alice@example.com", "alice-op"}, {"Bob@Example.COM", "shared-op"}, {"carol@example.com", "shared-op"}, {"dave@example.com", "operator"},
	} {
		if got := s.sessionWriteAs(c.email); got != c.want {
			t.Errorf("GET /auth/session for %s: actor %q, want %q", c.email, got, c.want)
		}
	}
	k := s.kinds()
	if len(k) != 3 || k["alice-op"] != "operator" || k["shared-op"] != "operator" || k["operator"] != "operator" {
		t.Errorf("kinds = %v, want alice-op, shared-op and operator, all operators", k)
	}
	if got := strings.Join(s.principals("shared-op"), ","); got != "oidc:Bob@Example.COM,oidc:carol@example.com" {
		t.Errorf("principals of shared-op = %q", got)
	}
	if got := strings.Join(s.principals("alice-op"), ","); got != "oidc:alice@example.com" {
		t.Errorf("principals of alice-op = %q", got)
	}
}

func TestAKeyWriteUnderAMappedHandleIsPromotedPerHandleAndNeverDemoted(t *testing.T) {
	t.Parallel()
	s := newKindServer(t)
	s.l.Operators = map[string]string{"alice@example.com": "alice-op", "bob@example.com": "bob-op"}
	s.keyWrite("alice-op")
	s.keyWrite("bob-op")
	s.sessionWriteAs("alice@example.com")
	if k := s.kinds(); k["alice-op"] != "operator" || k["bob-op"] != "agent" {
		t.Errorf("after alice's session write: %v, want alice-op an operator and bob-op still an agent", k)
	}
	s.sessionWriteAs("bob@example.com")
	s.keyWrite("alice-op")
	if k := s.kinds(); k["alice-op"] != "operator" || k["bob-op"] != "operator" {
		t.Errorf("after bob's session write and a key write as alice-op: %v, want both operators", k)
	}
}

func TestActorForMapsEmailsCaseInsensitivelyAndFallsBackToTheOperator(t *testing.T) {
	t.Parallel()
	l := &Login{Operator: "boss", Operators: map[string]string{"a@example.com": "ann"}}
	for email, want := range map[string]string{"a@example.com": "ann", "A@Example.com": "ann", "b@example.com": "boss", "": "boss"} {
		if got := l.ActorFor(email); got != want {
			t.Errorf("ActorFor(%q) = %q, want %q", email, got, want)
		}
	}
	if got := l.ActorFor("  a@example.com\t"); got != "ann" {
		t.Errorf("ActorFor with surrounding whitespace = %q, want ann", got)
	}
	if got := (&Login{Operator: "boss"}).ActorFor("a@example.com"); got != "boss" {
		t.Errorf("with no mapping ActorFor = %q, want boss", got)
	}
}
