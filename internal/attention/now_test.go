package attention

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/tasks"
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

func tagNames(marks []tasks.TagMark) []string {
	var out []string
	for _, m := range marks {
		out = append(out, m.Source+":"+m.Tag)
	}
	return out
}

func TestEveryTaskOfTheBoardCarriesItsOwnInitiativeAndActorTags(t *testing.T) {
	e := quiet(t)
	if _, err := e.Tasks.AddTag(ctx, op, "key:x", "aa-1", "mine"); err != nil {
		t.Fatal(err)
	}
	e.lease(t, audit.Actor{Handle: "claude:w1"}, "aa-1")
	ended := e.lease(t, audit.Actor{Handle: "ralph:r1"}, "aa-2")
	if _, err := e.Leases.End(ctx, me, "key:x", ended.Ref, "x"); err != nil {
		t.Fatal(err)
	}
	b := e.board(t, Query{}, "24h")
	flight, recent := b.InFlight[0].Task, b.Recent[0].Task
	if flight.Ref != "aa-1" || recent.Ref != "aa-2" {
		t.Fatalf("flight %s recent %s, want aa-1 and aa-2", flight.Ref, recent.Ref)
	}
	if !slices.Equal(flight.Tags, []string{"mine"}) || !slices.Equal(tagNames(flight.TagMarks), []string{"task:mine", "initiative:ops", "actor:claude"}) {
		t.Errorf("in flight task: tags %v marks %v", flight.Tags, tagNames(flight.TagMarks))
	}
	if len(recent.Tags) != 0 || recent.Tags == nil || !slices.Equal(tagNames(recent.TagMarks), []string{"initiative:ops", "actor:ralph"}) {
		t.Errorf("recent task: tags %v marks %v, want no own tags but an initiative and an actor mark", recent.Tags, tagNames(recent.TagMarks))
	}
	if got := b.TaskMarks["aa-1"]; !slices.Equal(got.Tags, flight.Tags) || !slices.Equal(tagNames(got.TagMarks), tagNames(flight.TagMarks)) {
		t.Errorf("task marks of aa-1 = %+v, want the in flight task's", got)
	}
}

func TestTheTaskMarksNameEveryTaskAnAttentionItemIsOnAndNoOther(t *testing.T) {
	e := quiet(t)
	if _, err := e.Tasks.AddTag(ctx, op, "key:x", "bb-1", "own-b"); err != nil {
		t.Fatal(err)
	}
	e.ask(t, me, asks.New{Task: "aa-2", Question: "which?"})
	e.ask(t, me, asks.New{Initiative: "bb", Question: "no task here"})
	e.lease(t, me, "aa-1", "repo:x")
	e.lease(t, bob, "bb-1", "repo:x")
	b := e.board(t, Query{}, "24h")
	if b.Attention.Counts.Conflicts != 1 || b.Attention.Counts.OpenAsks != 2 {
		t.Fatalf("counts = %+v, want a conflict and two asks", b.Attention.Counts)
	}
	var keys []string
	for ref := range b.TaskMarks {
		keys = append(keys, ref)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"aa-1", "aa-2", "bb-1"}) {
		t.Fatalf("task marks name %v, want aa-1 and aa-2 (ask, flight) and bb-1 (conflict) and no empty ref", keys)
	}
	if got := tagNames(b.TaskMarks["bb-1"].TagMarks); !slices.Equal(got, []string{"task:own-b"}) {
		t.Errorf("bb-1 marks = %v", got)
	}
	if got := b.TaskMarks["aa-2"]; got.Tags == nil || len(got.Tags) != 0 || !slices.Equal(tagNames(got.TagMarks), []string{"initiative:ops"}) {
		t.Errorf("aa-2 = %+v, want no own tags (an empty list) and the initiative's", got)
	}
}

func TestAnExpiredLeaseAndADoneTaskKeepTheirTagsOnTheBoard(t *testing.T) {
	e := quiet(t)
	if _, err := e.Tasks.AddTag(ctx, op, "key:x", "aa-1", "stranded-own"); err != nil {
		t.Fatal(err)
	}
	e.lease(t, audit.Actor{Handle: "pi:p1"}, "aa-1")
	e.tick(5 * time.Minute)
	b := e.board(t, Query{}, "8h")
	if b.Attention.Counts.ExpiredLeases != 1 {
		t.Fatalf("counts = %+v, want the expired lease", b.Attention.Counts)
	}
	if got := tagNames(b.TaskMarks["aa-1"].TagMarks); !slices.Equal(got, []string{"task:stranded-own", "initiative:ops", "actor:pi"}) {
		t.Errorf("an expired lease's task has marks %v", got)
	}
}

// itemMarks is the tags and marks of the attention items that are tasks, keyed by
// "<section> <ref>".
func itemMarks(r Result) map[string][]string {
	out := map[string][]string{}
	put := func(key string, tags []string, marks []tasks.TagMark) {
		if tags == nil {
			out[key] = []string{"nil tags"}
			return
		}
		out[key] = append(append([]string{}, tags...), tagNames(marks)...)
	}
	for _, d := range r.DoneNotClosed {
		put("done "+d.Task, d.Tags, d.TagMarks)
	}
	for _, l := range r.ExpiredLeases {
		put("expired "+l.Task, l.Tags, l.TagMarks)
	}
	for _, w := range r.OverdueWatches {
		put("watch "+w.Task, w.Tags, w.TagMarks)
	}
	return out
}

func TestEveryAttentionItemThatIsATaskCarriesItsOwnInitiativeAndActorTags(t *testing.T) {
	e := newEnv(t)
	cadence := int64(3600)
	w := must(e.Tasks.Create(ctx, bob, "key:x", tasks.New{Initiative: "aa", Title: "watch", Kind: "watch", CadenceSeconds: &cadence, Tags: []string{"w-own"}}))
	must(e.Tasks.AddTag(ctx, op, "key:x", "aa-1", "mine"))
	e.lease(t, audit.Actor{Handle: "ralph:r1"}, "bb-1")
	done := e.lease(t, audit.Actor{Handle: "claude:w1"}, "aa-1")
	must(e.Leases.End(ctx, me, "key:x", done.Ref, "x"))
	must(e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}))
	e.tick(90 * time.Minute)

	want := map[string][]string{
		"done aa-1":      {"mine", "task:mine", "initiative:ops", "actor:claude"},
		"expired bb-1":   {"actor:ralph"},
		"watch " + w.Ref: {"w-own", "task:w-own", "initiative:ops"},
	}
	if got := itemMarks(e.get(t, Query{})); !reflect.DeepEqual(got, want) {
		t.Errorf("GET attention items = %v, want %v", got, want)
	}
	if got := itemMarks(e.board(t, Query{}, "24h").Attention); !reflect.DeepEqual(got, want) {
		t.Errorf("board attention items = %v, want %v", got, want)
	}
}

// lastEventOrigins ends one lease per (task, ctx) pair, spaced a second apart, and returns the Recently touched
// rows' last-event actor and origin keyed by task ref.
func lastEventOrigins(t *testing.T, e env, endings []struct {
	task string
	as   audit.Actor
	ctx  func() context.Context
}) map[string][2]string {
	t.Helper()
	for _, n := range endings {
		l := e.lease(t, n.as, n.task)
		e.tick(time.Second)
		if _, err := e.Leases.End(n.ctx(), n.as, "key:x", l.Ref, "done"); err != nil {
			t.Fatal(err)
		}
	}
	out := map[string][2]string{}
	for _, r := range e.board(t, Query{}, "8h").Recent {
		out[r.Task.Ref] = [2]string{r.LastEvent.Actor.Handle, r.LastEvent.Origin}
	}
	return out
}

func TestTheRecentLastEventCarriesTheOriginTheEventRecordedWhateverTheActorIsNow(t *testing.T) {
	e := quiet(t)
	// Both agents act through an operator who is no actor row until something of theirs is written.
	e.tick(time.Second)
	if _, err := e.Tasks.Create(ctx, op, "key:x", tasks.New{Initiative: "bb", Title: "operator row exists"}); err != nil {
		t.Fatal(err)
	}
	via := func(h string) func() context.Context {
		return func() context.Context { return audit.WithOrigin(ctx, h) }
	}
	got := lastEventOrigins(t, e, []struct {
		task string
		as   audit.Actor
		ctx  func() context.Context
	}{
		{"aa-1", me, via(op.Handle)},                        // differing: an agent writing for an operator
		{"aa-2", op, func() context.Context { return ctx }}, // equal: an operator's own write, origin is itself
		{"bb-1", bob, via("")},                              // null: a key without an owner
	})
	if got["aa-1"] != [2]string{me.Handle, op.Handle} {
		t.Errorf("aa-1 (agent for an operator) = %v, want actor %s origin %s", got["aa-1"], me.Handle, op.Handle)
	}
	if got["aa-2"] != [2]string{op.Handle, op.Handle} {
		t.Errorf("aa-2 (operator's own write) = %v, want actor and origin both %s", got["aa-2"], op.Handle)
	}
	if got["bb-1"] != [2]string{bob.Handle, ""} {
		t.Errorf("bb-1 (no known operator) = %v, want actor %s and no origin", got["bb-1"], bob.Handle)
	}
	// The agent's origin is overwritten by a later ownerless write; the event written earlier keeps its own.
	l := e.lease(t, me, "bb-2")
	e.tick(time.Second)
	if _, err := e.Leases.End(audit.WithOrigin(ctx, ""), me, "key:x", l.Ref, "later"); err != nil {
		t.Fatal(err)
	}
	b := e.board(t, Query{}, "8h")
	for _, r := range b.Recent {
		switch r.Task.Ref {
		case "aa-1":
			if r.LastEvent.Origin != op.Handle {
				t.Errorf("aa-1 origin after the actor's origin was cleared = %q, want %s", r.LastEvent.Origin, op.Handle)
			}
		case "bb-2":
			if r.LastEvent.Origin != "" {
				t.Errorf("bb-2 origin = %q, want none", r.LastEvent.Origin)
			}
		}
	}
}
