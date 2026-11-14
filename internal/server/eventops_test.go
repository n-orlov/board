package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestEventReadsAnswer503WithoutAStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	list, _ := eventOps{}.ListEvents(ctx, ListEventsRequestObject{})
	task, _ := eventOps{}.ListTaskEvents(ctx, ListTaskEventsRequestObject{Ref: "aa-1"})
	for name, got := range map[string]any{"events": list, "task events": task} {
		p, ok := got.(problemResponse)
		if !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("%s without a store = %#v, want a 503 problem", name, got)
		}
	}
}

func TestListEventsReadsFiltersPagesAndShapesItems(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.task(`{"initiative":"aa","title":"two"}`)
	if rec := s.send(http.MethodPost, "/api/v1/tasks/aa-1/milestones", `{"summary":"halfway","data":{"pct":50}}`, "codex@host"); rec.Code != http.StatusOK {
		t.Fatalf("milestone = %d %s", rec.Code, rec.Body)
	}
	rec := s.send(http.MethodGet, "/api/v1/events?type=milestone&type=task_created&limit=2", "", "claude@host")
	doc := decode(t, rec)
	items, _ := doc["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 2 || doc["next_after"] == nil {
		t.Fatalf("page = %d %s", rec.Code, rec.Body)
	}
	first, _ := items[0].(map[string]any)
	if first["ref"] != "E2" || first["type"] != "task_created" || first["task"] != "aa-1" || first["initiative"] != "aa" ||
		first["principal"] != "key:caller" || first["actor"] != "claude@host" || first["ref_id"] == nil {
		t.Errorf("first item = %v", first)
	}
	next := s.send(http.MethodGet, fmt.Sprintf("/api/v1/events?type=milestone&type=task_created&limit=2&after=%v", doc["next_after"]), "", "claude@host")
	rest, _ := decode(t, next)["items"].([]any)
	if len(rest) != 1 || !strings.Contains(next.Body.String(), `"type":"milestone"`) || strings.Contains(next.Body.String(), "next_after") {
		t.Errorf("last page = %s", next.Body)
	}
}

func TestEventReadsRefuseWhatTheyCannotRead(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	for path, want := range map[string]int{
		"/api/v1/events?limit=0":                           http.StatusBadRequest,
		"/api/v1/events?limit=1001":                        http.StatusBadRequest,
		"/api/v1/events?type=":                             http.StatusBadRequest,
		"/api/v1/events?since=yesterday":                   http.StatusBadRequest,
		"/api/v1/events?after=2021-06-01T00:00:00Z":        http.StatusBadRequest,
		"/api/v1/events?initiative=zz":                     http.StatusNotFound,
		"/api/v1/events?task=aa-9":                         http.StatusNotFound,
		"/api/v1/tasks/aa-9/events":                        http.StatusNotFound,
		"/api/v1/tasks/aa-1/events?limit=0":                http.StatusBadRequest,
		"/api/v1/tasks/aa-1/events?after=nonsense":         http.StatusBadRequest,
		"/api/v1/tasks/aa-1/events?limit=1&after=E1":       http.StatusOK,
		"/api/v1/events?actor=nobody%40host&since=E1":      http.StatusOK,
		"/api/v1/events?since=2021-06-01T00:00:00%2B02:00": http.StatusOK,
	} {
		if rec := s.send(http.MethodGet, path, "", "claude@host"); rec.Code != want {
			t.Errorf("GET %s = %d %s, want %d", path, rec.Code, rec.Body, want)
		}
	}
}

func TestCheckAndMilestoneBodiesAreRequired(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	for _, sub := range []string{"checks", "milestones"} {
		for _, body := range []string{"", `{}`, `{"summary":""}`} {
			rec := s.send(http.MethodPost, "/api/v1/tasks/aa-1/"+sub, body, "claude@host")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST %s with %q = %d %s, want 400", sub, body, rec.Code, rec.Body)
			}
		}
	}
}
