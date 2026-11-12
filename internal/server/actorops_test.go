package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/roster"
)

func TestActorOperationsAnswer503WithoutAStore(t *testing.T) {
	o := actorOps{}
	list, err := o.ListActors(context.Background(), ListActorsRequestObject{})
	if p, ok := list.(problemResponse); err != nil || !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("ListActors = %#v, %v, want a 503 problem", list, err)
	}
	one, err := o.GetActor(context.Background(), GetActorRequestObject{Handle: "x"})
	if p, ok := one.(problemResponse); err != nil || !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("GetActor = %#v, %v, want a 503 problem", one, err)
	}
}

func TestActorsAreListedAndReadWithTheirLeasesAndAsks(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	if rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work","resources":["repo:b","repo:a"]}`, "codex@host"); rec.Code != http.StatusCreated {
		t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
	}
	if rec := s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-1","question":"Which?","mode":"poll","blocking":true}`, "codex@host"); rec.Code != http.StatusCreated {
		t.Fatalf("raise ask = %d %s", rec.Code, rec.Body)
	}
	rec := s.send(http.MethodGet, "/api/v1/actors?alive=true", "", "operator@host")
	doc := decode(t, rec)
	items, _ := doc["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 1 {
		t.Fatalf("alive list = %d %s, want only codex@host", rec.Code, rec.Body)
	}
	one := decode(t, s.send(http.MethodGet, "/api/v1/actors/codex@host", "", "operator@host"))
	lease := one["leases"].([]any)[0].(map[string]any)
	ask := one["asks"].([]any)[0].(map[string]any)
	if one["handle"] != "codex@host" || one["kind"] != "agent" || one["last_seen"] == nil || one["way_back"] != nil {
		t.Errorf("actor = %v", one)
	}
	if lease["ref"] != "L1" || lease["task"] != "aa-1" || lease["intent"] != "work" || lease["expires_at"] == nil || strings.Join(toStrings(lease["resources"]), ",") != "repo:a,repo:b" {
		t.Errorf("lease = %v", lease)
	}
	if ask["ref"] != "A1" || ask["task"] != "aa-1" || ask["initiative"] != "aa" || ask["question"] != "Which?" || ask["mode"] != "poll" || ask["blocking"] != true || ask["created_at"] == nil {
		t.Errorf("ask = %v", ask)
	}
}

func toStrings(v any) []string {
	out := []string{}
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

func TestAnUnknownActorIs404WithANextLine(t *testing.T) {
	s := newKeyServer(t)
	rec := s.send(http.MethodGet, "/api/v1/actors/nobody@host", "", "operator@host")
	doc := decode(t, rec)
	if rec.Code != http.StatusNotFound || !strings.Contains(doc["detail"].(string), "nobody@host") || doc["next"] == nil {
		t.Errorf("unknown actor = %d %v", rec.Code, doc)
	}
}

func TestAnActorWithAWayBackAnswersIt(t *testing.T) {
	a := apiActorView(roster.Actor{Handle: "x@host", Kind: "operator", WayBack: "ssh box"})
	if a.WayBack == nil || *a.WayBack != "ssh box" || a.Kind != Operator {
		t.Errorf("view = %+v", a)
	}
	if a.Leases == nil || a.Asks == nil {
		t.Errorf("lists = %v, %v, want empty and not null", a.Leases, a.Asks)
	}
}
