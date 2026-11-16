package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/checkin"
	"github.com/n-orlov/village-hall/internal/leases"
)

func TestAskRaisedCarriesEveryFieldOfTheAsk(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	s := func(v string) *string { return &v }
	ch := AskChannelRelay
	a := Ask{
		Answer: s("8080"), AnsweredAt: &at, AnsweredBy: s("nik"), Asker: "claude", AskerWaiting: true, Blocking: true,
		Channel: &ch, CreatedAt: at, Details: "d", EndedAt: &at, Id: "01X", Initiative: s("aa"), Mode: AskModePoll,
		OnBehalfOf: s("bo"), Options: []string{"a"}, Question: "q?", Ref: "A1", State: AskStateAnswered, Task: s("aa-1"),
		WithdrawReason: s("r"), WithdrawnAt: &at,
	}
	lease := &Lease{Ref: "L1"}
	got := askRaised(a, lease)
	want := AskRaised{
		Answer: a.Answer, AnsweredAt: a.AnsweredAt, AnsweredBy: a.AnsweredBy, Asker: "claude", AskerWaiting: true, Blocking: true,
		CreatedAt: at, Details: "d", EndedAt: a.EndedAt, Id: "01X", Initiative: a.Initiative, Lease: lease, Mode: AskRaisedModePoll,
		OnBehalfOf: a.OnBehalfOf, Options: []string{"a"}, Question: "q?", Ref: "A1", State: AskRaisedStateAnswered, Task: a.Task,
		WithdrawReason: a.WithdrawReason, WithdrawnAt: a.WithdrawnAt,
	}
	relay := AskRaisedChannelRelay
	want.Channel = &relay
	if got.Channel == nil || *got.Channel != relay {
		t.Fatalf("channel = %v, want relay", got.Channel)
	}
	got.Channel, want.Channel = nil, nil
	if got.Lease != lease || got.Answer != want.Answer || got.Ref != want.Ref || got.Mode != want.Mode || got.State != want.State ||
		got.Task != want.Task || got.Initiative != want.Initiative || got.Asker != want.Asker || got.Question != want.Question ||
		got.Details != want.Details || !got.AskerWaiting || !got.Blocking || got.Id != want.Id || got.OnBehalfOf != want.OnBehalfOf ||
		got.AnsweredBy != want.AnsweredBy || got.AnsweredAt != want.AnsweredAt || got.EndedAt != want.EndedAt ||
		got.WithdrawReason != want.WithdrawReason || got.WithdrawnAt != want.WithdrawnAt || !got.CreatedAt.Equal(at) || len(got.Options) != 1 {
		t.Errorf("askRaised = %+v, want %+v", got, want)
	}
	if out := askRaised(Ask{}, nil); out.Channel != nil || out.Lease != nil {
		t.Errorf("an ask with no channel and no lease = %+v, want neither", out)
	}
}

func TestOnlyKeepAndStopActOnTheLease(t *testing.T) {
	t.Parallel()
	of := func(a AskRaiseLeaseAction) *AskRaise { return &AskRaise{LeaseAction: &a} }
	for name, c := range map[string]struct {
		in   *AskRaise
		want bool
	}{
		"left out": {&AskRaise{}, false}, "none": {of(AskRaiseLeaseActionNone), false},
		"keep": {of(AskRaiseLeaseActionKeep), true}, "stop": {of(AskRaiseLeaseActionStop), true},
		"unknown": {of("pause"), true},
	} {
		if got := acts(c.in); got != c.want {
			t.Errorf("%s: acts = %v, want %v", name, got, c.want)
		}
	}
}

func TestALeaseActionWithoutALeaseStoreAnswers503(t *testing.T) {
	t.Parallel()
	ops := askOps{asks: &asks.Store{}}
	for _, action := range []AskRaiseLeaseAction{AskRaiseLeaseActionKeep, AskRaiseLeaseActionStop} {
		ctx := withIdentity(context.Background(), auth.Identity{Principal: "key:k", Actor: "a@h"})
		got, err := ops.RaiseAsk(ctx, RaiseAskRequestObject{Body: &AskRaise{Question: "q?", LeaseAction: &action}})
		if p, ok := got.(problemResponse); err != nil || !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("lease_action %s without leases = %#v, %v, want a 503 problem", action, got, err)
		}
	}
	full := askOps{asks: &asks.Store{}, block: checkin.Store{Leases: &leases.Store{}, Asks: &asks.Store{}}}
	if p, off := full.blockReady(); off || p.status != http.StatusServiceUnavailable {
		t.Errorf("every store present: off = %v, status %d", off, p.status)
	}
	if _, off := (askOps{block: checkin.Store{Leases: &leases.Store{}}}).blockReady(); !off {
		t.Error("without asks the block is not ready")
	}
}

func TestRaiseAskWithALeaseActionThroughTheHandler(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"mine"}`, "claude@host")

	rec := s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-1","question":"q?","lease_action":"keep"}`, "claude@host")
	doc := decode(t, rec)
	if lease, _ := doc["lease"].(map[string]any); rec.Code != http.StatusCreated || lease["state"] != "live" || doc["ref"] != "A1" {
		t.Errorf("keep = %d %s, want 201 with A1 and the live lease", rec.Code, rec.Body)
	}
	rec = s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-1","question":"q2?","lease_action":"stop"}`, "claude@host")
	doc = decode(t, rec)
	lease, _ := doc["lease"].(map[string]any)
	if rec.Code != http.StatusCreated || lease["state"] != "ended" || lease["outcome"] != "blocked on A2" {
		t.Errorf("stop = %d %s, want 201 with the lease ended as blocked on A2", rec.Code, rec.Body)
	}
	rec = s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-1","question":"q3?","lease_action":"stop"}`, "claude@host")
	if _, has := decode(t, rec)["lease"]; rec.Code != http.StatusCreated || has {
		t.Errorf("stop without a lease = %d %s, want 201 and no lease", rec.Code, rec.Body)
	}
	for _, action := range []string{"pause", "STOP"} {
		rec = s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-1","question":"q?","lease_action":"`+action+`"}`, "claude@host")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"next"`) {
			t.Errorf("lease_action %s = %d %s, want 400 with a next", action, rec.Code, rec.Body)
		}
	}
	rec = s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-9","question":"q?","lease_action":"stop"}`, "claude@host")
	if rec.Code != http.StatusNotFound {
		t.Errorf("stop on an unknown task = %d %s, want 404", rec.Code, rec.Body)
	}
}
