package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestListNotificationsAnswers503WithoutAStore(t *testing.T) {
	t.Parallel()
	got, err := notifyOps{}.ListNotifications(context.Background(), ListNotificationsRequestObject{})
	p, ok := got.(problemResponse)
	if err != nil || !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("without a store = %#v, %v, want a 503 problem", got, err)
	}
}

// seedNotifications writes three rows, one per state, on the events of three tasks, the oldest pending.
func seedNotifications(t *testing.T, s *keyServer) {
	t.Helper()
	s.create("aa", "")
	for i := 0; i < 3; i++ {
		s.task(`{"initiative":"aa","title":"t"}`)
	}
	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC).UnixMilli()
	rows := []struct{ id, kind, state, extra string }{
		{"01JC0000000000000000000001", "stranded", "pending", ""},
		{"01JC0000000000000000000002", "asking", "sent", ""},
		{"01JC0000000000000000000003", "done", "failed", ""},
	}
	for i, r := range rows {
		_, err := s.st.Writer().Exec(s.st.Dialect().Rebind(`INSERT INTO notifications (id, kind, event_id, payload, state, attempts, next_attempt_at, last_error, created_at, sent_at)
			VALUES (?, ?, (SELECT id FROM events WHERE type = 'task_created' ORDER BY n LIMIT 1 OFFSET ?), ?, ?, ?, ?, ?, ?, ?)`),
			r.id, r.kind, i, `{"kind":"`+r.kind+`"}`, r.state, i, at, map[bool]any{true: "http 500", false: nil}[r.state == "failed"], at+int64(i), map[bool]any{true: at + 9, false: nil}[r.state == "sent"])
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestListNotificationsFiltersPagesAndShapesItems(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	seedNotifications(t, s)
	idsAt := func(path string) (ids []string, doc map[string]any) {
		t.Helper()
		rec := s.send(http.MethodGet, path, "", "claude@host")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
		}
		doc = decode(t, rec)
		items, _ := doc["items"].([]any)
		for _, it := range items {
			ids = append(ids, it.(map[string]any)["id"].(string)[25:])
		}
		return ids, doc
	}
	for path, want := range map[string]string{
		"/api/v1/notifications": "3 2 1", "/api/v1/notifications?state=pending": "1", "/api/v1/notifications?state=sent": "2",
		"/api/v1/notifications?state=failed": "3", "/api/v1/notifications?limit=2": "3 2",
	} {
		if ids, _ := idsAt(path); strings.Join(ids, " ") != want {
			t.Errorf("%s = %v, want %s", path, ids, want)
		}
	}
	_, first := idsAt("/api/v1/notifications?limit=2")
	ids, last := idsAt(fmt.Sprintf("/api/v1/notifications?limit=2&after=%v", first["next_after"]))
	if strings.Join(ids, " ") != "1" || last["next_after"] != nil {
		t.Errorf("last page = %v %v", ids, last)
	}
	_, all := idsAt("/api/v1/notifications?state=failed")
	item := all["items"].([]any)[0].(map[string]any)
	if item["kind"] != "done" || item["state"] != "failed" || item["event"] == nil || item["last_error"] != "http 500" || item["attempts"] != float64(2) ||
		item["created_at"] != "2026-11-06T09:00:00.002Z" || item["next_attempt_at"] != nil || item["sent_at"] != nil || item["payload"].(map[string]any)["kind"] != "done" {
		t.Errorf("failed item = %v", item)
	}
	_, sent := idsAt("/api/v1/notifications?state=sent")
	if it := sent["items"].([]any)[0].(map[string]any); it["sent_at"] != "2026-11-06T09:00:00.009Z" || it["last_error"] != nil {
		t.Errorf("sent item = %v", it)
	}
}

func TestListNotificationsRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	seedNotifications(t, s)
	for _, q := range []string{"state=queued", "state=", "limit=0", "limit=1001", "limit=abc", "after=nope", "after=01JC0000000000000000000099"} {
		rec := s.send(http.MethodGet, "/api/v1/notifications?"+q, "", "claude@host")
		doc := decode(t, rec)
		if rec.Code != http.StatusBadRequest || doc["next"] == nil || doc["detail"] == nil {
			t.Errorf("%s = %d %s, want a 400 with a next", q, rec.Code, rec.Body)
		}
	}
}
