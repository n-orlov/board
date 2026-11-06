package leases

import (
	"reflect"
	"testing"
	"time"
)

func TestLiveOfListsOnlyTheLiveUnexpiredLeasesOfThatTaskOldestFirst(t *testing.T) {
	e := newEnv(t)
	first := e.start(t, me, "aa-1", secs(600), "r2", "r1")
	e.start(t, bob, "aa-2", secs(600))
	second := e.start(t, bob, "aa-1", secs(60))
	task, err := e.Tasks.Get(ctx, "aa-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.S.LiveOf(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Ref != first.Lease.Ref || got[1].Ref != second.Lease.Ref {
		t.Fatalf("LiveOf(aa-1) = %+v", got)
	}
	if !reflect.DeepEqual(got[0].Resources, []string{"r1", "r2"}) || got[1].Resources == nil {
		t.Errorf("resources = %v and %v", got[0].Resources, got[1].Resources)
	}
	if _, err := e.S.End(ctx, bob, "key:b", second.Lease.Ref, "done"); err != nil {
		t.Fatal(err)
	}
	if got, _ = e.S.LiveOf(ctx, task.ID); len(got) != 1 || got[0].Ref != first.Lease.Ref {
		t.Errorf("after an end, LiveOf = %+v", got)
	}
	// 601 s later the first lease has run out, though no sweep marked it: the read does.
	e.Clock.add(601 * time.Second)
	if got, _ = e.S.LiveOf(ctx, task.ID); got == nil || len(got) != 0 {
		t.Errorf("after expiry, LiveOf = %#v, want an empty list", got)
	}
	if st := mustGet(t, e, first.Lease.Ref).State; st != "expired" {
		t.Errorf("after LiveOf, state = %q, want expired", st)
	}
	if _, err := e.S.LiveOf(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	// the aa-2 lease of the same age expired with it: one event each, however many reads.
	if evs := e.events(t, EventExpired); len(evs) != 2 {
		t.Errorf("%d lease_expired events after two reads, want 2", len(evs))
	}
}

func TestLiveOfIncludesALeaseAtItsExactExpiry(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", secs(60)).Lease
	task, _ := e.Tasks.Get(ctx, "aa-1")
	e.Clock.now = l.ExpiresAt
	if got, err := e.S.LiveOf(ctx, task.ID); err != nil || len(got) != 1 {
		t.Errorf("at expires_at: %v, %v", got, err)
	}
	e.Clock.add(time.Millisecond)
	if got, _ := e.S.LiveOf(ctx, task.ID); len(got) != 0 {
		t.Errorf("1 ms after expires_at: %v", got)
	}
}

func mustGet(t *testing.T, e env, ref string) Lease {
	t.Helper()
	l, err := e.S.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLiveForGroupsTheLiveLeasesByTaskAndLeavesOutThoseThatRanOut(t *testing.T) {
	e := newEnv(t)
	first := e.start(t, me, "aa-1", secs(600), "r1")
	second := e.start(t, bob, "aa-1", secs(60))
	e.start(t, bob, "aa-2", secs(600))
	t1, _ := e.Tasks.Get(ctx, "aa-1")
	t2, _ := e.Tasks.Get(ctx, "aa-2")
	got, err := e.S.LiveFor(ctx, []string{t1.ID, t2.ID})
	if err != nil || len(got[t1.ID]) != 2 || len(got[t2.ID]) != 1 || got[t1.ID][0].Ref != first.Lease.Ref || got[t1.ID][1].Ref != second.Lease.Ref {
		t.Fatalf("LiveFor = %+v, %v", got, err)
	}
	if got[t1.ID][0].Resources[0] != "r1" {
		t.Errorf("resources = %v", got[t1.ID][0].Resources)
	}
	e.Clock.add(61 * time.Second)
	got, err = e.S.LiveFor(ctx, []string{t1.ID, t2.ID})
	if err != nil || len(got[t1.ID]) != 1 || len(got[t2.ID]) != 1 {
		t.Errorf("after the short ttl: %+v, %v", got, err)
	}
	if st := mustGet(t, e, second.Lease.Ref).State; st != "live" {
		t.Errorf("LiveFor changed the state to %q; only ExpireDue does", st)
	}
	if got, err := e.S.LiveFor(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("no ids = %+v, %v", got, err)
	}
}

func TestLiveCountsForCountsTheLiveLeasesOfEachInitiativeAndLeavesOutThoseThatRanOut(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", secs(600))
	e.start(t, bob, "aa-1", secs(60))
	e.start(t, bob, "aa-2", secs(600))
	t1, _ := e.Tasks.Get(ctx, "aa-1")
	got, err := e.S.LiveCountsFor(ctx, []string{t1.InitiativeID})
	if err != nil || len(got) != 1 || got[t1.InitiativeID] != 3 {
		t.Fatalf("LiveCountsFor = %+v, %v", got, err)
	}
	e.Clock.add(61 * time.Second)
	got, err = e.S.LiveCountsFor(ctx, []string{t1.InitiativeID, "unknown"})
	if err != nil || len(got) != 1 || got[t1.InitiativeID] != 2 {
		t.Errorf("after the short ttl: %+v, %v", got, err)
	}
	if got, err := e.S.LiveCountsFor(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("no ids = %+v, %v", got, err)
	}
}
