package checkin

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/tasks"
)

func TestAPingWithNoLiveLeaseIsAConflictThatWritesNothing(t *testing.T) {
	e := newEnv(t)
	for _, in := range []PingRequest{{}, {Progress: ptr("working")}} {
		before := e.finishState(t)
		_, err := e.S.Ping(ctx, me, "k", in)
		var c leases.ConflictError
		if !errors.As(err, &c) || c.Msg != "you hold no live lease" || !strings.Contains(c.Next, "vh start") {
			t.Errorf("ping %+v = %v, want the no-lease conflict with a next", in, err)
		}
		if got := e.finishState(t); got != before {
			t.Errorf("state = %s after a refused ping, were %s", got, before)
		}
	}
}

func TestAPingRenewsEveryLiveLeaseOfTheCallerAndOnlyThose(t *testing.T) {
	e := newEnv(t)
	e.addTask(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "one"})
	e.start(t, StartRequest{Task: "aa-2", Intent: "two"})
	if _, err := e.S.Leases.Start(ctx, bob, "k", leases.Start{Task: "aa-3", Intent: "theirs"}); err != nil {
		t.Fatal(err)
	}
	theirs, err := e.S.Leases.Get(ctx, "L3")
	if err != nil {
		t.Fatal(err)
	}
	n := e.eventCount(t)
	e.advance(time.Hour)
	r, err := e.S.Ping(ctx, me, "k", PingRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Leases) != 2 || r.Leases[0].Ref != "L1" || r.Leases[1].Ref != "L2" || len(r.Milestones) != 0 {
		t.Fatalf("ping = %+v, want L1 and L2 and no milestones", r)
	}
	want := e.Now.Add(2 * time.Hour)
	for _, l := range r.Leases {
		if l.State != "live" || !l.ExpiresAt.Equal(want) || !l.RenewedAt.Equal(*e.Now) {
			t.Errorf("lease %s = %+v, want live, renewed now and expiring at %v", l.Ref, l, want)
		}
	}
	if got := e.eventTypes(t, n); got != "lease_renewed,lease_renewed" {
		t.Errorf("events = %s, want one lease_renewed for each lease", got)
	}
	if after, _ := e.S.Leases.Get(ctx, "L3"); !after.ExpiresAt.Equal(theirs.ExpiresAt) {
		t.Errorf("another actor's lease moved from %v to %v", theirs.ExpiresAt, after.ExpiresAt)
	}
}

func TestAPingWithProgressWritesOneMilestoneOnEachLeasedTask(t *testing.T) {
	e := newEnv(t)
	e.addTask(t)
	for _, task := range []string{"aa-1", "aa-2", "aa-3"} {
		e.start(t, StartRequest{Task: task, Intent: "work"})
	}
	n := e.eventCount(t)
	r, err := e.S.Ping(ctx, me, "k", PingRequest{Progress: ptr("  half way ")})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Leases) != 3 || strings.Join(r.Milestones, ",") != "aa-1,aa-2,aa-3" {
		t.Errorf("ping = %+v, want three leases and a milestone on each task", r)
	}
	if got := e.eventTypes(t, n); got != "lease_renewed,lease_renewed,lease_renewed,milestone,milestone,milestone" {
		t.Errorf("events = %s", got)
	}
	var summary string
	err = e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT summary FROM events WHERE type = 'milestone' ORDER BY id LIMIT 1`).Scan(&summary)
	if err != nil || summary != "milestone on aa-1: half way" {
		t.Errorf("milestone summary = %q, %v", summary, err)
	}
}

func TestAPingRefusesAWrongProgressBeforeWritingAnything(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "mine"})
	before := e.finishState(t)
	for name, p := range map[string]string{"empty": "  ", "long": strings.Repeat("x", 201)} {
		_, err := e.S.Ping(ctx, me, "k", PingRequest{Progress: &p})
		var bad leases.InvalidError
		if !errors.As(err, &bad) || bad.Next == "" {
			t.Errorf("%s progress: err = %v, want an invalid with a next", name, err)
		}
	}
	if got := e.finishState(t); got != before {
		t.Errorf("state = %s after refusals, were %s", got, before)
	}
	// the refusal comes first: a caller with no lease and a wrong progress is a 400, not a conflict
	other := bob
	if _, err := e.S.Ping(ctx, other, "k", PingRequest{Progress: ptr("")}); !errors.Is(err, leases.ErrInvalid) {
		t.Errorf("wrong progress without a lease = %v, want invalid", err)
	}
}

func TestAPingDoesNotRenewAnExpiredLease(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "mine"})
	e.advance(3 * time.Hour)
	if _, err := e.S.Ping(ctx, me, "k", PingRequest{}); !errors.Is(err, leases.ErrConflict) {
		t.Errorf("ping on an expired lease = %v, want a conflict", err)
	}
}

// addTask creates task aa-3.
func (e env) addTask(t *testing.T) {
	t.Helper()
	if _, err := e.S.Tasks.Create(ctx, me, "k", tasks.New{Initiative: "aa", Title: "third"}); err != nil {
		t.Fatal(err)
	}
}

func ptr(s string) *string { return &s }
