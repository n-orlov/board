package checkin

import (
	"errors"
	"testing"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
)

func (e env) askCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT count(*) FROM asks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func question(action string) asks.New {
	return asks.New{Task: "aa-1", Question: "Which port?", LeaseAction: action}
}

func TestABlockWithStopEndsTheLeaseAsBlockedOnTheAskInTheSameTransaction(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
	e.start(t, StartRequest{Task: "aa-2", Intent: "other"})
	n := e.eventCount(t)
	r, err := e.S.Block(ctx, me, "k", question(domain.LeaseActionStop))
	if err != nil {
		t.Fatal(err)
	}
	if r.Ask.Ref != "A1" || r.Lease == nil || r.Lease.Ref != "L1" || r.Lease.State != "ended" || r.Lease.Outcome != "blocked on A1" {
		t.Fatalf("block = %+v, want A1 and L1 ended as blocked on A1", r)
	}
	if got := e.eventTypes(t, n); got != "ask_raised,lease_ended" {
		t.Errorf("events = %s, want ask_raised and lease_ended", got)
	}
	if other, err := e.S.Leases.Get(ctx, "L2"); err != nil || other.State != "live" {
		t.Errorf("the lease on another task = %+v, %v, want it live", other, err)
	}
}

func TestABlockWithStopFindsNoLeaseOfAnotherActorOrOnNoTaskAndStillRaisesTheAsk(t *testing.T) {
	for name, tc := range map[string]struct {
		holder func(*env, *testing.T)
		in     asks.New
	}{
		"no lease at all":    {func(*env, *testing.T) {}, question("stop")},
		"another actor's":    {func(e *env, t *testing.T) { e.startAs(t, bob, "aa-1") }, question("stop")},
		"a lease on another": {func(e *env, t *testing.T) { e.startAs(t, me, "aa-2") }, question("stop")},
		"an ask with no task": {func(e *env, t *testing.T) { e.startAs(t, me, "aa-1") },
			asks.New{Initiative: "aa", Question: "Ship it?", LeaseAction: "stop"}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			tc.holder(&e, t)
			live := e.liveLeases(t)
			n := e.eventCount(t)
			r, err := e.S.Block(ctx, me, "k", tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if r.Ask.Ref != "A1" || r.Lease != nil {
				t.Errorf("block = %+v, want the ask and no lease", r)
			}
			if got := e.eventTypes(t, n); got != "ask_raised" {
				t.Errorf("events = %s, want only ask_raised", got)
			}
			if got := e.liveLeases(t); got != live {
				t.Errorf("%d live leases, were %d", got, live)
			}
		})
	}
}

func TestABlockWithKeepLeavesTheLeaseLiveAndAnswersIt(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
	n := e.eventCount(t)
	r, err := e.S.Block(ctx, me, "k", question(domain.LeaseActionKeep))
	if err != nil {
		t.Fatal(err)
	}
	if r.Ask.Ref != "A1" || r.Lease == nil || r.Lease.Ref != "L1" || r.Lease.State != "live" || r.Lease.Outcome != "" {
		t.Errorf("block = %+v, want A1 and L1 live", r)
	}
	if got := e.eventTypes(t, n); got != "ask_raised" {
		t.Errorf("events = %s, want only ask_raised", got)
	}
}

func TestABlockWithNoneOrNoActionOnlyRaisesTheAsk(t *testing.T) {
	for _, action := range []string{"", domain.LeaseActionNone} {
		e := newEnv(t)
		e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
		r, err := e.S.Block(ctx, me, "k", question(action))
		if err != nil || r.Ask.Ref != "A1" || r.Lease != nil || e.liveLeases(t) != 1 {
			t.Errorf("block with action %q = %+v, %v, live %d, want the ask and the lease untouched", action, r, err, e.liveLeases(t))
		}
	}
}

func TestABlockWithAnUnknownActionWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
	before := e.finishState(t)
	_, err := e.S.Block(ctx, me, "k", question("pause"))
	var bad asks.InvalidError
	if !errors.As(err, &bad) || bad.Next == "" {
		t.Errorf("block = %v, want an invalid with a next", err)
	}
	if got := e.finishState(t); got != before || e.askCount(t) != 0 {
		t.Errorf("state = %s with %d asks after a refusal, was %s", got, e.askCount(t), before)
	}
}

func TestABlockTheAskRefusesLeavesTheLeaseLive(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
	before := e.finishState(t)
	_, err := e.S.Block(ctx, me, "k", asks.New{Task: "aa-9", Question: "Which port?", LeaseAction: "stop"})
	if !errors.Is(err, asks.ErrNotFound) {
		t.Errorf("block on an unknown task = %v, want not found", err)
	}
	if got := e.finishState(t); got != before || e.askCount(t) != 0 {
		t.Errorf("state = %s with %d asks after a refusal, was %s", got, e.askCount(t), before)
	}
}

// startAs starts a lease of the actor on the task.
func (e *env) startAs(t *testing.T, who audit.Actor, task string) {
	t.Helper()
	if _, err := e.S.Start(ctx, who, "k", StartRequest{Task: task, Intent: "work"}); err != nil {
		t.Fatal(err)
	}
}

func (e env) liveLeases(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT count(*) FROM leases WHERE state = 'live'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
