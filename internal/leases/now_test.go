package leases_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/tasks"
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

// A check-in that finds no lease due is a read: it must not need the SQLite
// writer connection, which another request may hold (SPEC 14).
func TestNowWithNothingDueDoesNotTakeTheWriterConnection(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", secs(600), "r1")
	held, err := e.S.Store.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	short, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	got, err := e.S.CheckIn(short, me.Handle, nil)
	if err != nil || len(got.Mine) != 1 {
		t.Fatalf("CheckIn with the writer held = %d of mine, %v; want 1 and no error", len(got.Mine), err)
	}
	if _, err := e.S.Live(short); err != nil {
		t.Errorf("Live with the writer held: %v", err)
	}
}

// Resources are read for many leases at once: every lease gets its own, sorted,
// across the chunks a long list is read in.
func TestLiveLeasesCarryTheirOwnResourcesAcrossChunks(t *testing.T) {
	e := newEnv(t)
	const n = 520
	for i := 0; i < n; i++ {
		e.Clock.add(time.Millisecond)
		task, err := e.Tasks.Create(ctx, me, "key:x", tasks.New{Initiative: "aa", Title: "t"})
		if err != nil {
			t.Fatal(err)
		}
		e.start(t, me, task.Ref, secs(600), fmt.Sprintf("r%04d-b", i), fmt.Sprintf("r%04d-a", i))
	}
	e.start(t, bob, "aa-1", secs(600))
	live, err := e.S.Live(ctx)
	if err != nil || len(live) != n+1 {
		t.Fatalf("Live = %d leases, %v; want %d", len(live), err, n+1)
	}
	for i, l := range live[:n] {
		want := []string{fmt.Sprintf("r%04d-a", i), fmt.Sprintf("r%04d-b", i)}
		if !reflect.DeepEqual(l.Resources, want) {
			t.Fatalf("lease %s resources = %v, want %v", l.Ref, l.Resources, want)
		}
	}
	if last := live[n]; last.Resources == nil || len(last.Resources) != 0 {
		t.Errorf("a lease with no resources has %#v, want an empty non-nil list", last.Resources)
	}
}
