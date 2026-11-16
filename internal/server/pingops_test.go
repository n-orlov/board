package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/checkin"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/tasks"
)

func TestPingAnswers503WithoutItsStores(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Leases: &leases.Store{}, Tasks: &tasks.Store{}}
	if p, off := (pingOps{checkin: full}).ready(); off || p.status != http.StatusServiceUnavailable {
		t.Errorf("every store present: off = %v, status %d", off, p.status)
	}
	for name, drop := range map[string]func(*checkin.Store){
		"leases": func(c *checkin.Store) { c.Leases = nil }, "tasks": func(c *checkin.Store) { c.Tasks = nil },
	} {
		c := full
		drop(&c)
		got, _ := pingOps{checkin: c}.Ping(context.Background(), PingRequestObject{})
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("without %s = %#v, want a 503 problem", name, got)
		}
	}
}

func TestPingWithoutAPrincipalIsRefused(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Leases: &leases.Store{}, Tasks: &tasks.Store{}}
	got, _ := pingOps{checkin: full}.Ping(context.Background(), PingRequestObject{})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("ping without a principal = %#v, want 401", got)
	}
}

func TestPingRoundTripThroughTheHandler(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.task(`{"initiative":"aa","title":"two"}`)

	rec := s.send(http.MethodPost, "/api/v1/ping", "", "claude@host")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"next"`) {
		t.Fatalf("ping with no lease = %d %s, want 409 with a next", rec.Code, rec.Body)
	}
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"mine"}`, "claude@host")
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-2","intent":"mine too"}`, "claude@host")

	for _, body := range []string{"", `{}`} {
		rec = s.send(http.MethodPost, "/api/v1/ping", body, "claude@host")
		doc := decode(t, rec)
		if rec.Code != http.StatusOK || len(doc["leases"].([]any)) != 2 || len(doc["milestones"].([]any)) != 0 {
			t.Errorf("ping %q = %d %s, want 200 with two leases and no milestones", body, rec.Code, rec.Body)
		}
	}
	rec = s.send(http.MethodPost, "/api/v1/ping", `{"progress":"half way"}`, "claude@host")
	doc := decode(t, rec)
	ms := doc["milestones"].([]any)
	if rec.Code != http.StatusOK || len(ms) != 2 || ms[0] != "aa-1" || ms[1] != "aa-2" {
		t.Errorf("ping with progress = %d %s, want milestones on aa-1 and aa-2", rec.Code, rec.Body)
	}
	if first := doc["leases"].([]any)[0].(map[string]any); first["ref"] != "L1" || first["state"] != "live" {
		t.Errorf("first lease = %v", first)
	}
	for _, body := range []string{`{"progress":""}`, `{"progress":"` + strings.Repeat("x", 201) + `"}`} {
		if rec := s.send(http.MethodPost, "/api/v1/ping", body, "claude@host"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"next"`) {
			t.Errorf("ping %.30s = %d %s, want 400 with a next", body, rec.Code, rec.Body)
		}
	}
}
