package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sendWith is send with extra request headers.
func (s *keyServer) sendWith(method, path, body, actor string, extra map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set(ActorHeader, actor)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extra {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, r)
	return rec
}

// actorRow is the persisted state of one actor, or ok=false when it has no row.
func (s *keyServer) actorRow(handle string) (wayBack sql.NullString, meta string, ok bool) {
	s.t.Helper()
	err := s.keys.Store.Reader().QueryRowContext(context.Background(), `SELECT way_back, meta FROM actors WHERE handle = ?`, handle).Scan(&wayBack, &meta)
	if errors.Is(err, sql.ErrNoRows) {
		return wayBack, "", false
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return wayBack, meta, true
}

func (s *keyServer) countActors() int {
	s.t.Helper()
	var n int
	if err := s.keys.Store.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM actors`).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

func TestWriteStoresTheActorWayBackAndMetaFromItsHeaders(t *testing.T) {
	s := newKeyServer(t)
	hdr := map[string]string{WayBackHeader: " tmux attach -t ws1 ", MetaHeader: `{"cwd":"/src"}`}
	if rec := s.sendWith(http.MethodPost, "/api/v1/initiatives", `{"slug":"one","title":"t"}`, "claude@host", hdr); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	wb, meta, ok := s.actorRow("claude@host")
	if !ok || wb.String != "tmux attach -t ws1" || meta != `{"cwd":"/src"}` {
		t.Fatalf("actor way_back=%v meta=%q ok=%v", wb, meta, ok)
	}
	// A later write that sends neither header leaves both alone ...
	s.sendWith(http.MethodPost, "/api/v1/initiatives", `{"slug":"two","title":"t"}`, "claude@host", nil)
	if wb, meta, _ = s.actorRow("claude@host"); wb.String != "tmux attach -t ws1" || meta != `{"cwd":"/src"}` {
		t.Errorf("a write with no headers changed way_back=%v meta=%q", wb, meta)
	}
	// ... and one that sends a new way back replaces it.
	s.sendWith(http.MethodPost, "/api/v1/initiatives", `{"slug":"three","title":"t"}`, "claude@host", map[string]string{WayBackHeader: "https://example.test/s/9"})
	if wb, meta, _ = s.actorRow("claude@host"); wb.String != "https://example.test/s/9" || meta != `{"cwd":"/src"}` {
		t.Errorf("way_back=%v meta=%q after a new way back", wb, meta)
	}
	if ev := s.events(); len(ev) != 3 || ev[2].principal != "key:caller" || ev[2].actor != "claude@host" {
		t.Errorf("events keep principal and actor: %+v", ev)
	}
}

func TestProtocolSeenAlsoStoresTheActorHeaders(t *testing.T) {
	s := newKeyServer(t)
	rec := s.sendWith(http.MethodPut, "/api/v1/actors/me/protocol-seen", `{"version":0}`, "ralph:run-42", map[string]string{WayBackHeader: "pid 7 on h"})
	if rec.Code != http.StatusOK {
		t.Fatalf("protocol-seen = %d %s", rec.Code, rec.Body)
	}
	if wb, meta, ok := s.actorRow("ralph:run-42"); !ok || wb.String != "pid 7 on h" || meta != "{}" {
		t.Errorf("way_back=%v meta=%q ok=%v", wb, meta, ok)
	}
}

func TestReadsCreateNoActorRowInPhaseZero(t *testing.T) {
	s := newKeyServer(t)
	for _, path := range []string{"/api/v1/meta", "/api/v1/initiatives", "/api/v1/keys"} {
		if rec := s.sendWith(http.MethodGet, path, "", "reader@host", map[string]string{WayBackHeader: "somewhere"}); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
		}
	}
	if n := s.countActors(); n != 0 {
		t.Errorf("reads left %d actor rows; SPEC 4.1 says only writes record an actor in Phase 0", n)
	}
}

func TestMetaHeaderThatIsNotAJSONObjectIs400AndRecordsNothing(t *testing.T) {
	for _, meta := range []string{"not json", "[1]", `"s"`, "null", "7"} {
		s := newKeyServer(t)
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/v1/meta", ""},
			{http.MethodPost, "/api/v1/initiatives", `{"slug":"one","title":"t"}`},
		} {
			rec := s.sendWith(c.method, c.path, c.body, "a@h", map[string]string{MetaHeader: meta})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s with meta %q = %d", c.method, c.path, meta, rec.Code)
			}
			p := problem(t, rec)
			if d, _ := p["detail"].(string); !strings.Contains(d, MetaHeader) || p["next"] == "" {
				t.Errorf("problem %v", p)
			}
		}
		if n := s.countActors(); n != 0 {
			t.Errorf("meta %q left %d actors", meta, n)
		}
	}
}
