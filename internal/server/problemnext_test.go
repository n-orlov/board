package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeProblem(t *testing.T, label string, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("%s: Content-Type = %q, want application/problem+json", label, ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("%s: body %q is not JSON: %v", label, rec.Body.String(), err)
	}
	if int(doc["status"].(float64)) != rec.Code {
		t.Errorf("%s: problem status %v, response %d", label, doc["status"], rec.Code)
	}
	if next, _ := doc["next"].(string); strings.TrimSpace(next) == "" {
		t.Errorf("%s: problem %v has no next", label, doc)
	}
	for _, f := range []string{"type", "title", "detail"} {
		if s, _ := doc[f].(string); s == "" {
			t.Errorf("%s: problem %v lost %s", label, doc, f)
		}
	}
	return doc
}

// TestEveryErrorPathOfTheHandlerAnswersAProblemWithNext drives the real handler
// down the exceptional paths of each kind: an operation's own typed problem
// (unknown Swagger asset), an unreadable parameter, an unreadable body, an
// unknown key, a store that is not wired, and refused credentials.
func TestEveryErrorPathOfTheHandlerAnswersAProblemWithNext(t *testing.T) {
	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"unknown asset", authed("/api/docs/assets/nope.js"), 404},
		{"unknown asset, other name", authed("/api/docs/assets/..%2Fsecret"), 404},
		{"bad limit", authed("/api/v1/initiatives?limit=abc"), 400},
		{"malformed body", authedWith("POST", "/api/v1/keys", "{"), 400},
		{"initiatives without a store", authed("/api/v1/initiatives"), 503},
		{"keys without a store", authed("/api/v1/keys"), 503},
		{"login not configured", authed("/auth/login"), 404},
		{"no credential", httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil), 401},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, tc.req)
		if rec.Code != tc.want {
			t.Errorf("%s: %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
			continue
		}
		decodeProblem(t, tc.name, rec)
	}
}

func TestUnknownDocsAssetKeepsItsOwnNextAndFields(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, authed("/api/docs/assets/nope.js"))
	doc := decodeProblem(t, "asset", rec)
	if doc["detail"] != "there is no such documentation asset" || doc["title"] != "Not Found" || doc["type"] != problemType {
		t.Errorf("problem = %v", doc)
	}
	if !strings.Contains(doc["next"].(string), "/api/docs") {
		t.Errorf("next = %q, want advice naming /api/docs", doc["next"])
	}
}

// TestWithProblemNextCompletesAnyProblemAHandlerWrites covers the wrapper on its
// own, for statuses and shapes no current operation answers.
func TestWithProblemNextCompletesAnyProblemAHandlerWrites(t *testing.T) {
	write := func(status int, body string, explicitHeader bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
			w.Header().Set("Content-Length", "999")
			if explicitHeader {
				w.WriteHeader(status)
			}
			_, _ = w.Write([]byte(body))
		})
	}
	cases := []struct {
		name     string
		h        http.Handler
		want     int
		wantNext string
	}{
		{"no next, 409", write(409, `{"type":"about:blank","title":"Conflict","status":409,"detail":"d"}`, true), 409, defaultNext(409)},
		{"empty next, 502", write(502, `{"type":"about:blank","title":"Bad Gateway","status":502,"detail":"d","next":""}`, true), 502, defaultNext(502)},
		{"null next, 418", write(418, `{"type":"about:blank","title":"t","status":418,"detail":"d","next":null}`, true), 418, defaultNext(418)},
		{"own next kept", write(400, `{"type":"about:blank","title":"t","status":400,"detail":"d","next":"Do this."}`, true), 400, "Do this."},
		{"implicit status", write(200, `{"type":"about:blank","title":"t","status":200,"detail":"d"}`, false), 200, defaultNext(200)},
		{"body not JSON", write(500, `oops`, true), 500, defaultNext(500)},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		withProblemNext(tc.h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, rec.Code, tc.want)
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: body %q: %v", tc.name, rec.Body.String(), err)
		}
		if doc["next"] != tc.wantNext {
			t.Errorf("%s: next = %v, want %q", tc.name, doc["next"], tc.wantNext)
		}
		if rec.Header().Get("Content-Length") == "999" {
			t.Errorf("%s: a stale Content-Length survived", tc.name)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("%s: Content-Type = %q", tc.name, ct)
		}
	}
}

func TestWithProblemNextLeavesOtherResponsesAlone(t *testing.T) {
	h := withProblemNext(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "3")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("tea"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot || rec.Body.String() != "tea" || rec.Header().Get("Content-Length") != "3" {
		t.Errorf("response = %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Length"))
	}
}
