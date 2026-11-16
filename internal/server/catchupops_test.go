package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/checkin"
	"github.com/n-orlov/village-hall/internal/cursors"
	"github.com/n-orlov/village-hall/internal/notes"
)

func TestCatchupAnswers503WithoutItsStores(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Cursors: &cursors.Store{}, Notes: &notes.Store{}, Asks: &asks.Store{}}
	if p, off := (catchupOps{checkin: full}).ready(); off || p.status != http.StatusServiceUnavailable {
		t.Errorf("every store present: off = %v, status %d", off, p.status)
	}
	for name, drop := range map[string]func(*checkin.Store){
		"cursors": func(c *checkin.Store) { c.Cursors = nil }, "notes": func(c *checkin.Store) { c.Notes = nil },
		"asks": func(c *checkin.Store) { c.Asks = nil },
	} {
		c := full
		drop(&c)
		got, _ := catchupOps{checkin: c}.Catchup(context.Background(), CatchupRequestObject{})
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("without %s = %#v, want a 503 problem", name, got)
		}
	}
}

func TestCatchupWithoutAPrincipalIsRefused(t *testing.T) {
	t.Parallel()
	full := checkin.Store{Cursors: &cursors.Store{}, Notes: &notes.Store{}, Asks: &asks.Store{}}
	got, _ := catchupOps{checkin: full}.Catchup(context.Background(), CatchupRequestObject{})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("catchup without a principal = %#v, want 401", got)
	}
}

func TestCatchupRoundTripThroughTheHandler(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	rec := s.send(http.MethodPost, "/api/v1/catchup", "", "claude@host")
	doc := decode(t, rec)
	if rec.Code != http.StatusOK || len(doc["notes"].([]any)) != 0 || len(doc["answers"].([]any)) != 0 || doc["notes_more"] != float64(0) {
		t.Fatalf("catchup of nothing = %d %s, want 200 with empty lists", rec.Code, rec.Body)
	}
	s.send(http.MethodPost, "/api/v1/notes", `{"summary":"hello"}`, "nik@phone")
	s.send(http.MethodPost, "/api/v1/asks", `{"question":"which?","mode":"poll"}`, "claude@host")
	s.send(http.MethodPost, "/api/v1/asks/A1/answer", `{"answer":"this one"}`, "nik@phone")

	rec = s.send(http.MethodPost, "/api/v1/catchup", "", "claude@host")
	doc = decode(t, rec)
	ns, as := doc["notes"].([]any), doc["answers"].([]any)
	if rec.Code != http.StatusOK || len(ns) != 1 || ns[0].(map[string]any)["ref"] != "N1" || len(as) != 1 || as[0].(map[string]any)["answer"] != "this one" {
		t.Fatalf("catchup = %d %s, want note N1 and the answer", rec.Code, rec.Body)
	}
	rec = s.send(http.MethodPost, "/api/v1/catchup", "", "claude@host")
	doc = decode(t, rec)
	if len(doc["notes"].([]any)) != 0 || len(doc["answers"].([]any)) != 0 {
		t.Errorf("second catchup = %s, want nothing", rec.Body)
	}
}
