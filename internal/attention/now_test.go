package attention

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
)

func (e env) board(t *testing.T, q Query, window string) Board {
	t.Helper()
	b, err := e.A.Board(ctx, q, window)
	if err != nil {
		t.Fatalf("board %+v %s: %v", q, window, err)
	}
	return b
}

// quiet is a board whose tasks were all created long ago, so that only what a test
// does afterwards counts as touching a task.
func quiet(t *testing.T) env {
	t.Helper()
	e := newEnv(t)
	e.tick(100 * time.Hour)
	return e
}

func refsOf(b Board) (flight, recent []string) {
	for _, f := range b.InFlight {
		flight = append(flight, f.Task.Ref)
	}
	for _, r := range b.Recent {
		recent = append(recent, r.Task.Ref)
	}
	return flight, recent
}

func TestWindowSpanKnowsExactlyThreeWindows(t *testing.T) {
	for window, want := range map[string]time.Duration{"8h": 8 * time.Hour, "24h": 24 * time.Hour, "3d": 72 * time.Hour} {
		if got, ok := WindowSpan(window); !ok || got != want {
			t.Errorf("WindowSpan(%q) = %v, %v; want %v", window, got, ok, want)
		}
	}
	for _, bad := range []string{"", "1h", "7d", "24H", "72h"} {
		if _, ok := WindowSpan(bad); ok {
			t.Errorf("WindowSpan(%q) is a window", bad)
		}
	}
}

func TestABadWindowIsInvalidAndAnswersNoBoard(t *testing.T) {
	e := newEnv(t)
	b, err := e.A.Board(ctx, Query{}, "2d")
	var bad InvalidError
	if !errors.As(err, &bad) || !errors.Is(err, ErrInvalid) || bad.Next != NextWindow {
		t.Fatalf("err = %v, want InvalidError with next %q", err, NextWindow)
	}
	if b.InFlight != nil || b.Recent != nil {
		t.Errorf("a refused read answered %+v", b)
	}
}

func TestAnEmptyBoardHasEmptyPartsNeverNil(t *testing.T) {
	e := quiet(t)
	b := e.board(t, Query{}, "24h")
	if b.Window != "24h" || b.Attention.Counts.Total != 0 {
		t.Errorf("board = %+v", b)
	}
	if b.InFlight == nil {
		t.Errorf("in flight is nil, want an empty list")
	}
}

func TestALiveLeasePutsItsTaskInFlightWithTheLeaseNested(t *testing.T) {
	e := quiet(t)
	l := e.lease(t, me, "aa-1")
	b := e.board(t, Query{}, "24h")
	flight, recent := refsOf(b)
	if !slices.Equal(flight, []string{"aa-1"}) || len(recent) != 0 {
		t.Fatalf("flight %v recent %v, want aa-1 in flight alone", flight, recent)
	}
	got := b.InFlight[0]
	if got.Task.Title != "task in aa" || got.Task.Initiative != "aa" || got.Task.Status != "open" || got.Task.Kind != "work" {
		t.Errorf("task = %+v", got.Task)
	}
	if len(got.Leases) != 1 || got.Leases[0].Lease != l.Ref || got.Leases[0].Holder.Handle != me.Handle ||
		got.Leases[0].Holder.WayBack != me.WayBack || got.Leases[0].Intent != "work on aa-1" || !got.Leases[0].ExpiresAt.Equal(l.ExpiresAt) {
		t.Errorf("leases = %+v, want %s held by %s", got.Leases, l.Ref, me.Handle)
	}
}

func TestTwoLiveLeasesOnOneTaskAreNestedInLeaseOrderAndTheNewestTaskComesFirst(t *testing.T) {
	e := quiet(t)
	first := e.lease(t, me, "aa-1")
	second := e.lease(t, bob, "aa-1")
	third := e.lease(t, me, "bb-1")
	b := e.board(t, Query{}, "24h")
	flight, _ := refsOf(b)
	if !slices.Equal(flight, []string{"bb-1", "aa-1"}) {
		t.Fatalf("flight = %v, want the task with the newest lease first", flight)
	}
	nested := b.InFlight[1].Leases
	if len(nested) != 2 || nested[0].Lease != first.Ref || nested[1].Lease != second.Ref || b.InFlight[0].Leases[0].Lease != third.Ref {
		t.Errorf("nested = %+v, want %s then %s", nested, first.Ref, second.Ref)
	}
}

func TestAnEndedLeaseInsideTheWindowMakesItsTaskRecentAndOutsideItDoesNot(t *testing.T) {
	e := quiet(t)
	l := e.lease(t, me, "aa-1")
	e.tick(time.Second)
	if _, err := e.Leases.End(ctx, me, "key:x", l.Ref, "wrapped up"); err != nil {
		t.Fatal(err)
	}
	if flight, recent := refsOf(e.board(t, Query{}, "8h")); len(flight) != 0 || !slices.Equal(recent, []string{"aa-1"}) {
		t.Fatalf("flight %v recent %v, want aa-1 recent only", flight, recent)
	}
	b := e.board(t, Query{}, "8h")
	if ev := b.Recent[0].LastEvent; ev.Type != "lease_ended" || ev.Actor.Handle != me.Handle || ev.Summary == "" || !ev.At.Equal(e.Clock.now) {
		t.Errorf("last event = %+v", ev)
	}
	e.tick(9 * time.Hour)
	if _, recent := refsOf(e.board(t, Query{}, "8h")); len(recent) != 0 {
		t.Errorf("recent after 9h in the 8h window = %v", recent)
	}
	if _, recent := refsOf(e.board(t, Query{}, "24h")); !slices.Equal(recent, []string{"aa-1"}) {
		t.Errorf("recent after 9h in the 24h window = %v", recent)
	}
	e.tick(70 * time.Hour)
	if _, recent := refsOf(e.board(t, Query{}, "3d")); len(recent) != 0 {
		t.Errorf("recent after 79h in the 3d window = %v", recent)
	}
}

func TestATaskWithALiveLeaseIsNeverRecentEvenWithAnEndedLease(t *testing.T) {
	e := quiet(t)
	old := e.lease(t, me, "aa-1")
	if _, err := e.Leases.End(ctx, me, "key:x", old.Ref, "pause"); err != nil {
		t.Fatal(err)
	}
	e.lease(t, bob, "aa-1")
	flight, recent := refsOf(e.board(t, Query{}, "24h"))
	if !slices.Equal(flight, []string{"aa-1"}) || len(recent) != 0 {
		t.Errorf("flight %v recent %v, want aa-1 in flight and not recent", flight, recent)
	}
}

func TestALeaseThatRanOutLeavesFlightAndTheTaskBecomesRecent(t *testing.T) {
	e := quiet(t)
	e.lease(t, me, "aa-1")
	e.tick(2 * time.Minute)
	flight, recent := refsOf(e.board(t, Query{}, "8h"))
	if len(flight) != 0 || !slices.Equal(recent, []string{"aa-1"}) {
		t.Errorf("flight %v recent %v, want aa-1 recent after its lease expired", flight, recent)
	}
}

func TestRecentTasksComeLatestEventFirst(t *testing.T) {
	e := quiet(t)
	a := e.lease(t, me, "aa-1")
	b := e.lease(t, me, "aa-2")
	for _, l := range []string{b.Ref, a.Ref} {
		e.tick(time.Second)
		if _, err := e.Leases.End(ctx, me, "key:x", l, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, recent := refsOf(e.board(t, Query{}, "24h")); !slices.Equal(recent, []string{"aa-1", "aa-2"}) {
		t.Errorf("recent = %v, want aa-1 (ended last) then aa-2", recent)
	}
}

func TestTheFiltersNarrowTheAttentionTheFlightAndTheRecentParts(t *testing.T) {
	e := quiet(t)
	e.lease(t, audit.Actor{Handle: "claude:w1"}, "aa-1")
	ended := e.lease(t, me, "bb-1")
	if _, err := e.Leases.End(ctx, me, "key:x", ended.Ref, "x"); err != nil {
		t.Fatal(err)
	}
	e.ask(t, me, asks.New{Task: "aa-2", Question: "which?"})
	e.ask(t, me, asks.New{Task: "bb-1", Question: "which one?"})
	b := e.board(t, Query{}, "24h")
	if flight, recent := refsOf(b); !slices.Equal(flight, []string{"aa-1"}) || len(recent) != 2 || b.Attention.Counts.OpenAsks != 2 {
		t.Fatalf("unfiltered: flight %v recent %v asks %d", flight, recent, b.Attention.Counts.OpenAsks)
	}
	for name, tc := range map[string]struct {
		q                    Query
		flight, recent       int
		asks                 int
		firstFlight, aRecent string
	}{
		"initiative aa": {Query{Initiatives: []string{"aa"}}, 1, 1, 1, "aa-1", "aa-2"},
		"initiative bb": {Query{Initiatives: []string{"bb"}}, 0, 1, 1, "", "bb-1"},
		"tag ops":       {Query{Tags: []string{"ops"}}, 1, 1, 1, "aa-1", "aa-2"},
		"both":          {Query{Initiatives: []string{"bb"}, Tags: []string{"ops"}}, 0, 0, 0, "", ""},
		"claude kind":   {Query{Tags: []string{"claude"}}, 1, 0, 0, "aa-1", ""},
	} {
		got := e.board(t, tc.q, "24h")
		flight, recent := refsOf(got)
		if len(flight) != tc.flight || len(recent) != tc.recent || got.Attention.Counts.OpenAsks != tc.asks {
			t.Errorf("%s: flight %v recent %v asks %d", name, flight, recent, got.Attention.Counts.OpenAsks)
			continue
		}
		if tc.firstFlight != "" && flight[0] != tc.firstFlight {
			t.Errorf("%s: flight %v, want %s", name, flight, tc.firstFlight)
		}
		if tc.aRecent != "" && !slices.Contains(recent, tc.aRecent) {
			t.Errorf("%s: recent %v lacks %s", name, recent, tc.aRecent)
		}
	}
}

func TestABoardReadRefusesABadTagAndAnUnknownInitiative(t *testing.T) {
	e := newEnv(t)
	if _, err := e.A.Board(ctx, Query{Tags: []string{"bad tag"}}, "24h"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad tag: %v", err)
	}
	if _, err := e.A.Board(ctx, Query{Initiatives: []string{"zz"}}, "24h"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown initiative: %v", err)
	}
}
