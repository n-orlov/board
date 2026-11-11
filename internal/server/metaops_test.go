package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetaReportsTheServerAndTheCaller(t *testing.T) {
	s := newKeyServer(t)
	rec := s.send(http.MethodGet, "/api/v1/meta", "", "claude@host")
	if rec.Code != http.StatusOK {
		t.Fatalf("meta = %d %s", rec.Code, rec.Body)
	}
	got := decode(t, rec)
	want := map[string]any{"version": "v9.9.9", "revision": "abc123def456", "protocol": float64(7), "principal": "key:caller", "actor": "claude@host"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("meta %s = %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
}

func (s *keyServer) protocolSeen(handle string) *int {
	s.t.Helper()
	var seen *int
	err := s.keys.Store.Reader().QueryRowContext(context.Background(), `SELECT protocol_seen FROM actors WHERE handle = ?`, handle).Scan(&seen)
	if err != nil {
		s.t.Fatal(err)
	}
	return seen
}

func TestProtocolSeenRecordsTheHighestVersionForTheCallingActor(t *testing.T) {
	s := newKeyServer(t)
	for _, c := range []struct {
		send string
		want float64
	}{{`{"version":0}`, 0}, {`{"version":5}`, 5}, {`{"version":2}`, 5}} {
		rec := s.send(http.MethodPut, "/api/v1/actors/me/protocol-seen", c.send, "claude@host")
		if rec.Code != http.StatusOK {
			t.Fatalf("put %s = %d %s", c.send, rec.Code, rec.Body)
		}
		got := decode(t, rec)
		if got["actor"] != "claude@host" || got["protocol_seen"] != c.want {
			t.Fatalf("put %s answered %v, want actor claude@host and protocol_seen %v", c.send, got, c.want)
		}
	}
	if seen := s.protocolSeen("claude@host"); seen == nil || *seen != 5 {
		t.Fatalf("stored protocol_seen %v, want 5", seen)
	}
}

func TestProtocolSeenRefusesABadBodyWithANextAction(t *testing.T) {
	s := newKeyServer(t)
	for _, body := range []string{`{"version":-1}`, ``, `{"version":"x"}`} {
		rec := s.send(http.MethodPut, "/api/v1/actors/me/protocol-seen", body, "claude@host")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("put %q = %d %s, want 400", body, rec.Code, rec.Body)
		}
	}
	rec := s.send(http.MethodPut, "/api/v1/actors/me/protocol-seen", `{"version":-1}`, "claude@host")
	if next, _ := decode(t, rec)["next"].(string); next == "" {
		t.Errorf("the 400 has no next action: %s", rec.Body)
	}
}

func TestMetaAndProtocolSeenNeedTheActorHeader(t *testing.T) {
	s := newKeyServer(t)
	if rec := s.send(http.MethodGet, "/api/v1/meta", "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("meta without Board-Actor = %d, want 400", rec.Code)
	}
	if rec := s.send(http.MethodPut, "/api/v1/actors/me/protocol-seen", `{"version":1}`, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("protocol-seen without Board-Actor = %d, want 400", rec.Code)
	}
}

func TestProtocolSeenAnswers503WithoutAnActorsStore(t *testing.T) {
	h := NewHandler(ok, func(context.Context, string) (string, error) { return "k", nil }, nil, nil)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/actors/me/protocol-seen", strings.NewReader(`{"version":1}`))
	r.Header.Set("Authorization", "Bearer brd_x")
	r.Header.Set(ActorHeader, "claude@host")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s, want 503", rec.Code, rec.Body)
	}
}

func TestMetaOmitsTheRevisionOfABuildThatRecordedNone(t *testing.T) {
	orig := keyServerBuild
	keyServerBuild = Build{Version: "devel", Protocol: 7}
	t.Cleanup(func() { keyServerBuild = orig })
	s := newKeyServer(t)
	rec := s.send(http.MethodGet, "/api/v1/meta", "", "claude@host")
	if rec.Code != http.StatusOK {
		t.Fatalf("meta = %d %s", rec.Code, rec.Body)
	}
	got := decode(t, rec)
	if _, present := got["revision"]; present || got["version"] != "devel" {
		t.Errorf("meta of a build with no revision: %v, want version devel and no revision", got)
	}
}
