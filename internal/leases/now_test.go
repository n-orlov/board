package leases_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/leases"
)

func leaseRefs(list []leases.Lease) []string {
	out := []string{}
	for _, l := range list {
		out = append(out, l.Ref)
	}
	return out
}

func TestNowSplitsTheLiveLeasesIntoMineAndOthersWithTitles(t *testing.T) {
	e := newEnv(t)
	empty, err := e.S.CheckIn(ctx, me.Handle, nil)
	if err != nil || empty.Mine == nil || empty.Others == nil || empty.Conflicts == nil ||
		len(empty.Mine)+len(empty.Others)+len(empty.Conflicts) != 0 {
		t.Fatalf("no leases: %#v, %v; want three empty non-nil lists", empty, err)
	}
	mine := e.start(t, me, "aa-1", secs(600), "r1").Lease
	theirs := e.start(t, bob, "aa-2", secs(600), "r2").Lease
	got, err := e.S.CheckIn(ctx, me.Handle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(leaseRefs(got.Mine), []string{mine.Ref}) || !reflect.DeepEqual(leaseRefs(got.Others), []string{theirs.Ref}) {
		t.Fatalf("mine %v, others %v", leaseRefs(got.Mine), leaseRefs(got.Others))
	}
	if got.Mine[0].Title == "" || got.Mine[0].Task != "aa-1" || !reflect.DeepEqual(got.Mine[0].Resources, []string{"r1"}) {
		t.Errorf("my lease = %+v, want the task ref, a title and its resources", got.Mine[0])
	}
	if len(got.Conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none", got.Conflicts)
	}
}

func TestNowReportsConflictsOnMyResourcesAndOnTheQueriedOnes(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", secs(600), "shared", "mine")
	other := e.start(t, bob, "aa-2", secs(600), "shared", "other").Lease
	queried := e.start(t, bob, "aa-1", secs(600), "wanted").Lease
	got, err := e.S.CheckIn(ctx, me.Handle, []string{"wanted", "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Conflicts) != 2 || got.Conflicts[0].Lease != other.Ref || got.Conflicts[1].Lease != queried.Ref {
		t.Fatalf("conflicts = %+v, want %s then %s", got.Conflicts, other.Ref, queried.Ref)
	}
	if !reflect.DeepEqual(got.Conflicts[0].Resources, []string{"shared"}) || !reflect.DeepEqual(got.Conflicts[1].Resources, []string{"wanted"}) {
		t.Errorf("shared resources = %v and %v", got.Conflicts[0].Resources, got.Conflicts[1].Resources)
	}
}

func TestNowExpiresTheDueLeasesWithOneEventAndLeavesThemOut(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", secs(60), "r")
	e.start(t, bob, "aa-2", secs(600), "r")
	e.Clock.add(61 * time.Second)
	for i := 0; i < 2; i++ {
		got, err := e.S.CheckIn(ctx, me.Handle, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Mine) != 0 || len(got.Others) != 1 || len(got.Conflicts) != 0 {
			t.Fatalf("read %d: mine %d, others %d, conflicts %d; want 0, 1, 0", i, len(got.Mine), len(got.Others), len(got.Conflicts))
		}
	}
	if evs := e.events(t, leases.EventExpired); len(evs) != 1 {
		t.Errorf("%d lease_expired events after two reads, want 1", len(evs))
	}
}
