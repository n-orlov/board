package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/checkin"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/notes"
	"github.com/n-orlov/village-hall/internal/tasks"
)

func TestFinishAnswers503WithoutItsStores(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Leases: &leases.Store{}, Notes: &notes.Store{}, Tasks: &tasks.Store{}}
	if p, off := (finishOps{checkin: full}).ready(); off || p.status != http.StatusServiceUnavailable {
		t.Errorf("every store present: off = %v, status %d", off, p.status)
	}
	for name, drop := range map[string]func(*checkin.Store){
		"leases": func(c *checkin.Store) { c.Leases = nil }, "notes": func(c *checkin.Store) { c.Notes = nil },
		"tasks": func(c *checkin.Store) { c.Tasks = nil },
	} {
		c := full
		drop(&c)
		got, _ := finishOps{checkin: c}.Finish(context.Background(), FinishRequestObject{})
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("without %s = %#v, want a 503 problem", name, got)
		}
	}
}

func TestFinishWithoutAPrincipalOrABodyIsRefused(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Leases: &leases.Store{}, Notes: &notes.Store{}, Tasks: &tasks.Store{}}
	got, _ := finishOps{checkin: full}.Finish(context.Background(), FinishRequestObject{Body: &FinishRequest{Outcome: "x"}})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("finish without a principal = %#v, want 401", got)
	}
}

func TestFinishRoundTripThroughTheHandler(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.task(`{"initiative":"aa","title":"two"}`)
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"mine"}`, "claude@host")

	rec := s.send(http.MethodPost, "/api/v1/finish", `{"outcome":"shipped it"}`, "claude@host")
	doc := decode(t, rec)
	if rec.Code != http.StatusOK || doc["done"] != false {
		t.Fatalf("finish = %d %s, want 200", rec.Code, rec.Body)
	}
	lease, note := doc["lease"].(map[string]any), doc["note"].(map[string]any)
	if lease["ref"] != "L1" || lease["state"] != "ended" || lease["outcome"] != "shipped it" || note["summary"] != "shipped it" || note["task"] != "aa-1" {
		t.Errorf("lease %v, note %v", lease, note)
	}

	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-2","intent":"again"}`, "claude@host")
	rec = s.send(http.MethodPost, "/api/v1/finish", `{"task":"aa-2","outcome":"all done","done":true}`, "claude@host")
	if rec.Code != http.StatusOK || decode(t, rec)["done"] != true {
		t.Fatalf("finish with done = %d %s", rec.Code, rec.Body)
	}
	if got := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-2", "", "claude@host")); got["status"] != "done" {
		t.Errorf("task status = %v, want done", got["status"])
	}

	for body, want := range map[string]int{
		`{"outcome":"x"}`:                  http.StatusConflict,
		`{"task":"aa-1","outcome":"x"}`:    http.StatusConflict,
		`{"task":"aa-9","outcome":"x"}`:    http.StatusNotFound,
		`{"outcome":""}`:                   http.StatusBadRequest,
		`{"task":"aa-1","outcome":"a\nb"}`: http.StatusBadRequest,
	} {
		if rec := s.send(http.MethodPost, "/api/v1/finish", body, "claude@host"); rec.Code != want || !strings.Contains(rec.Body.String(), `"next"`) {
			t.Errorf("finish %s = %d %s, want %d with a next", body, rec.Code, rec.Body, want)
		}
	}
	if rec := s.send(http.MethodPost, "/api/v1/finish", "", "claude@host"); rec.Code != http.StatusBadRequest {
		t.Errorf("finish with no body = %d, want 400", rec.Code)
	}

	s.send(http.MethodPost, "/api/v1/tasks", `{"initiative":"aa","title":"three"}`, "claude@host")
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-3","intent":"mine"}`, "claude@host")
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-3","intent":"theirs"}`, "codex@host")
	rec = s.send(http.MethodPost, "/api/v1/finish", `{"task":"aa-3","outcome":"x","done":true}`, "claude@host")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "codex@host") {
		t.Errorf("done under another's lease = %d %s, want 409 naming the holder", rec.Code, rec.Body)
	}
}
