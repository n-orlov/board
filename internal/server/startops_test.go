package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/checkin"
	"github.com/n-orlov/village-hall/internal/cursors"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/notes"
	"github.com/n-orlov/village-hall/internal/tasks"
)

func TestStartAnswers503WithoutAllItsStores(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Leases: &leases.Store{}, Asks: &asks.Store{}, Notes: &notes.Store{}, Cursors: &cursors.Store{}, Attention: &attention.Store{}, Tasks: &tasks.Store{}}
	if p, off := (startOps{checkin: full}).ready(); off || p.status != http.StatusServiceUnavailable {
		t.Errorf("every store present: off = %v, status %d", off, p.status)
	}
	for name, drop := range map[string]func(*checkin.Store){
		"leases": func(c *checkin.Store) { c.Leases = nil }, "asks": func(c *checkin.Store) { c.Asks = nil },
		"notes": func(c *checkin.Store) { c.Notes = nil }, "cursors": func(c *checkin.Store) { c.Cursors = nil },
		"attention": func(c *checkin.Store) { c.Attention = nil }, "tasks": func(c *checkin.Store) { c.Tasks = nil },
	} {
		c := full
		drop(&c)
		got, _ := startOps{checkin: c}.Start(context.Background(), StartRequestObject{})
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("without %s = %#v, want a 503 problem", name, got)
		}
	}
}

func TestStartWithoutAPrincipalOrABodyIsRefused(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Leases: &leases.Store{}, Asks: &asks.Store{}, Notes: &notes.Store{}, Cursors: &cursors.Store{}, Attention: &attention.Store{}, Tasks: &tasks.Store{}}
	got, _ := startOps{checkin: full}.Start(context.Background(), StartRequestObject{Body: &StartRequest{Task: ptr("aa-1"), Intent: "x"}})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("start without a principal = %#v, want 401", got)
	}
}

func TestStartFailureMapsLeaseTaskAndServerErrors(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		err    error
		status int
	}{
		"invalid lease":      {leases.InvalidError{Msg: "x", Next: "n"}, http.StatusBadRequest},
		"done task":          {leases.ConflictError{Msg: "x", Next: "n"}, http.StatusConflict},
		"unknown task":       {errors.Join(leases.ErrNotFound), http.StatusNotFound},
		"closed initiative":  {errors.Join(tasks.ErrInitiativeClosed), http.StatusConflict},
		"unknown initiative": {errors.Join(tasks.ErrNoInitiative), http.StatusNotFound},
		"invalid task":       {tasks.InvalidError{Msg: "x", Next: "n"}, http.StatusBadRequest},
	} {
		p, ok := startFailure(c.err)
		if !ok || p.status != c.status {
			t.Errorf("%s: %d, %v; want %d", name, p.status, ok, c.status)
		}
	}
	if _, ok := startFailure(errors.New("disk")); ok {
		t.Error("a server error was mapped to a problem")
	}
}

func TestStartRoundTripThroughTheHandler(t *testing.T) {
	t.Parallel()
	b := keyServerBuild
	b.Protocol = 7
	s := newKeyServerOfBuild(t, b)
	s.create("aa", "")
	s.create("bb", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"theirs","resources":["db:x"]}`, "codex@host")
	s.send(http.MethodPut, "/api/v1/actors/me/protocol-seen", `{"version":5}`, "claude@host")

	rec := s.send(http.MethodPost, "/api/v1/start", `{"task":"aa-1","intent":"mine","ttl_seconds":600,"resources":["db:x"]}`, "claude@host")
	doc := decode(t, rec)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start = %d %s, want 201", rec.Code, rec.Body)
	}
	if doc["protocol"] != float64(7) || doc["protocol_seen"] != float64(5) || doc["task_created"] != false {
		t.Errorf("protocol, protocol_seen or task_created wrong: %s", rec.Body)
	}
	lease := doc["lease"].(map[string]any)
	if lease["ref"] != "L2" || lease["task"] != "aa-1" || lease["ttl_seconds"] != float64(600) {
		t.Errorf("lease = %v", lease)
	}
	if c := doc["conflicts"].([]any); len(c) != 1 || c[0].(map[string]any)["lease"] != "L1" {
		t.Errorf("conflicts = %v, want L1", c)
	}
	if now := doc["now"].(map[string]any); now["actor"] != "claude@host" || len(now["my_leases"].([]any)) != 1 {
		t.Errorf("now = %v", now)
	}

	rec = s.send(http.MethodPost, "/api/v1/start", `{"task":"aa-1","intent":"again"}`, "claude@host")
	if rec.Code != http.StatusOK || decode(t, rec)["lease"].(map[string]any)["ref"] != "L2" {
		t.Errorf("renewing start = %d %s, want 200 on L2", rec.Code, rec.Body)
	}

	rec = s.send(http.MethodPost, "/api/v1/start", `{"new_task":{"initiative":"bb","title":"fresh"},"intent":"go"}`, "claude@host")
	doc = decode(t, rec)
	if rec.Code != http.StatusCreated || doc["task_created"] != true || doc["lease"].(map[string]any)["task"] != "bb-1" {
		t.Errorf("new task start = %d %s", rec.Code, rec.Body)
	}

	for body, want := range map[string]int{
		`{"task":"aa-9","intent":"x"}`:                              http.StatusNotFound,
		`{"new_task":{"initiative":"zz","title":"t"},"intent":"x"}`: http.StatusNotFound,
		`{"intent":"x"}`:              http.StatusBadRequest,
		`{"task":"aa-1","intent":""}`: http.StatusBadRequest,
		`{"new_task":{"initiative":"bb","title":""},"intent":"x"}`: http.StatusBadRequest,
	} {
		if rec := s.send(http.MethodPost, "/api/v1/start", body, "claude@host"); rec.Code != want || !strings.Contains(rec.Body.String(), `"next"`) {
			t.Errorf("start %s = %d %s, want %d with a next", body, rec.Code, rec.Body, want)
		}
	}
	if rec := s.send(http.MethodPost, "/api/v1/start", "", "claude@host"); rec.Code != http.StatusBadRequest {
		t.Errorf("start with no body = %d, want 400", rec.Code)
	}
}
