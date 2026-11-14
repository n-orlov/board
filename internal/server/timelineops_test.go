package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/timeline"
)

func TestGetTimelineAnswers503WithoutItsStores(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]timelineOps{"no operation": {}, "no leases": {timeline: &timeline.Store{}}, "no timeline": {leases: &leases.Store{}}} {
		got, err := o.GetTimeline(context.Background(), GetTimelineRequestObject{})
		p, ok := got.(problemResponse)
		if err != nil || !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("%s = %#v, %v, want a 503 problem", name, got, err)
		}
	}
}

func timelineAt(s *keyServer, query url.Values) (int, map[string]any) {
	s.t.Helper()
	if query.Get("from") == "" {
		query.Set("from", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339))
		query.Set("to", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	}
	rec := s.send(http.MethodGet, "/api/v1/timeline?"+query.Encode(), "", "claude@host")
	return rec.Code, decode(s.t, rec)
}

func TestGetTimelineAnswersTheWindowWithAttentionFromTheDerivation(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", `,"tags":["web"]`)
	s.task(`{"initiative":"aa","title":"stranded one"}`)
	s.task(`{"initiative":"aa","title":"quiet one"}`)
	s.task(`{"initiative":"aa","title":"planned one","planned_start":"2026-01-01T00:00:00Z","planned_end":"2026-01-02T00:00:00Z"}`)
	for _, body := range []string{`{"task":"aa-1","intent":"work"}`} {
		if rec := s.send(http.MethodPost, "/api/v1/leases", body, "codex@host"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
		}
	}
	// the lease ran out a minute ago: the task is stranded
	if _, err := s.st.Writer().Exec(s.st.Dialect().Rebind(`UPDATE leases SET expires_at = ?`), store.EncodeTime(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	code, doc := timelineAt(s, url.Values{})
	if code != http.StatusOK {
		t.Fatalf("timeline = %d %v", code, doc)
	}
	tasks, _ := doc["tasks"].([]any)
	if len(tasks) != 3 {
		t.Fatalf("tasks = %v, want aa-1, aa-2 and aa-3 (open, so in the window from its creation)", tasks)
	}
	first, second, third := tasks[0].(map[string]any), tasks[1].(map[string]any), tasks[2].(map[string]any)
	if third["planned_start"] != "2026-01-01T00:00:00Z" || third["planned_end"] != "2026-01-02T00:00:00Z" || third["closed_at"] != nil {
		t.Errorf("planned task = %v", third)
	}
	att, _ := first["attention"].([]any)
	if first["ref"] != "aa-1" || len(att) != 1 || att[0].(map[string]any)["reason"] != "stranded" {
		t.Errorf("stranded task = %v", first)
	}
	if att2, ok := second["attention"].([]any); !ok || len(att2) != 0 || second["ref"] != "aa-2" {
		t.Errorf("quiet task = %v, want an empty attention list", second)
	}
	lease := doc["leases"].([]any)[0].(map[string]any)
	if lease["state"] != "expired" || lease["holder"] != "codex@host" || lease["task"] != "aa-1" || lease["ended_at"] == nil {
		t.Errorf("lease = %v", lease)
	}
	ini := doc["initiatives"].([]any)[0].(map[string]any)
	if ini["slug"] != "aa" || ini["closed"] != false || ini["tags"].([]any)[0] != "web" {
		t.Errorf("initiative = %v", ini)
	}
	if doc["from"] == nil || doc["to"] == nil || doc["markers"] == nil {
		t.Errorf("document = %v", doc)
	}
}

func TestGetTimelineWithoutAnAttentionStoreListsNoReasons(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	got, err := timelineOps{timeline: &timeline.Store{Store: s.st}, leases: &leases.Store{Store: s.st}}.GetTimeline(context.Background(), GetTimelineRequestObject{})
	ok, isOK := got.(GetTimeline200JSONResponse)
	if err != nil || !isOK || len(ok.Tasks) != 1 || ok.Tasks[0].Attention == nil || len(ok.Tasks[0].Attention) != 0 {
		t.Errorf("answer = %#v, %v", got, err)
	}
}

func TestGetTimelineRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	for q, want := range map[string]int{
		"from=yesterday": 400, "to=2000-01-01T00:00:00Z": 400, "from=2030-01-01T00:00:00Z&to=2030-01-01T00:00:00Z": 400,
		"from=2030-01-01T00:00:00Z&to=2035-01-01T00:00:00Z": 400, "closed_within=soon": 400, "tag=Bad%20Tag": 400, "initiative=nope": 404,
	} {
		rec := s.send(http.MethodGet, "/api/v1/timeline?"+q, "", "claude@host")
		doc := decode(t, rec)
		if rec.Code != want || doc["next"] == nil || doc["detail"] == nil {
			t.Errorf("%s = %d %s, want %d with a next", q, rec.Code, rec.Body, want)
		}
	}
	rec := s.send(http.MethodGet, "/api/v1/timeline?initiative=nope", "", "claude@host")
	if d := decode(t, rec)["detail"]; d != "there is no initiative nope" {
		t.Errorf("404 detail = %v", d)
	}
	if rec := s.send(http.MethodGet, "/api/v1/timeline?from=x", "", "claude@host"); !strings.Contains(rec.Body.String(), "RFC 3339") {
		t.Errorf("400 body = %s", rec.Body)
	}
}
