package tasks

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func depRows(t *testing.T, s Store) int {
	t.Helper()
	return count(t, s, `SELECT COUNT(*) FROM task_deps`)
}

func TestAddDepWritesRowAndOneDepAddedEventOnTheDependent(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	got, err := s.AddDep(ctx, audit2("codex@host"), "key:dep", a.Ref, b.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || !got.Blocked {
		t.Errorf("returned %+v", got)
	}
	ev := events(t, s, EventDepAdded)
	if len(ev) != 1 || ev[0].TaskID != a.ID || ev[0].RefID != a.ID || ev[0].InitiativeID != a.InitiativeID ||
		ev[0].Principal != "key:dep" || ev[0].Data["blocked_by"] != "aa-2" || ev[0].Data["ref"] != "aa-1" ||
		!strings.Contains(ev[0].Summary, "aa-1") || !strings.Contains(ev[0].Summary, "aa-2") {
		t.Errorf("events = %+v", ev)
	}
	if n := depRows(t, s); n != 1 {
		t.Errorf("task_deps rows = %d", n)
	}
	if other, _ := s.Get(ctx, b.Ref); other.Blocked {
		t.Error("the blocker became blocked")
	}
}

func TestAddDepAcceptsULIDsAndEitherLetterCase(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	if _, err := s.AddDep(ctx, who, "key:dep", "AA-1", strings.ToLower(b.ID)); err != nil {
		t.Fatal(err)
	}
	// the same dependency spelled by ref is a duplicate
	if _, err := s.AddDep(ctx, who, "key:dep", a.ID, "aa-2"); err != nil {
		t.Fatal(err)
	}
	if n := len(events(t, s, EventDepAdded)); n != 1 || depRows(t, s) != 1 {
		t.Errorf("events %d rows %d", n, depRows(t, s))
	}
}

func TestAddDepTwiceSucceedsAndWritesNoEventRowOrActor(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	if _, err := s.AddDep(ctx, who, "key:dep", a.Ref, b.Ref); err != nil {
		t.Fatal(err)
	}
	actors, events0 := count(t, s, `SELECT COUNT(*) FROM actors`), count(t, s, `SELECT COUNT(*) FROM events`)
	got, err := s.AddDep(ctx, audit2("new@host"), "key:other", a.Ref, b.Ref)
	if err != nil || !got.Blocked || got.ID != a.ID {
		t.Fatalf("duplicate add = %+v, %v", got, err)
	}
	if count(t, s, `SELECT COUNT(*) FROM actors`) != actors || count(t, s, `SELECT COUNT(*) FROM events`) != events0 || depRows(t, s) != 1 {
		t.Error("a duplicate add wrote something")
	}
}

func TestRemoveDepWritesDepRemovedAndUnblocks(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	if _, err := s.AddDep(ctx, who, "key:dep", a.Ref, b.Ref); err != nil {
		t.Fatal(err)
	}
	got, err := s.RemoveDep(ctx, who, "key:rm", a.Ref, b.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Blocked || depRows(t, s) != 0 {
		t.Errorf("after remove: %+v rows %d", got, depRows(t, s))
	}
	ev := events(t, s, EventDepGone)
	if len(ev) != 1 || ev[0].TaskID != a.ID || ev[0].Principal != "key:rm" || ev[0].Data["blocked_by"] != "aa-2" || ev[0].Data["ref"] != "aa-1" {
		t.Errorf("events = %+v", ev)
	}
}

func TestRemoveMissingDepIsNotFoundAndWritesNothing(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	events0, actors := count(t, s, `SELECT COUNT(*) FROM events`), count(t, s, `SELECT COUNT(*) FROM actors`)
	// the dependency exists the other way round only
	if _, err := s.AddDep(ctx, who, "key:dep", b.Ref, a.Ref); err != nil {
		t.Fatal(err)
	}
	events0++
	for _, tc := range [][2]string{{a.Ref, b.Ref}, {a.Ref, "aa-9"}, {"aa-9", a.Ref}, {"nonsense", a.Ref}} {
		if _, err := s.RemoveDep(ctx, audit2("new@host"), "key:rm", tc[0], tc[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("remove %v = %v, want ErrNotFound", tc, err)
		}
	}
	if count(t, s, `SELECT COUNT(*) FROM events`) != events0 || count(t, s, `SELECT COUNT(*) FROM actors`) != actors || depRows(t, s) != 1 {
		t.Error("a refused remove wrote something")
	}
}

func TestAddDepUnknownTaskIsNotFound(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a := mk(t, s, "aa", "a")
	for _, tc := range [][2]string{{a.Ref, "aa-9"}, {"aa-9", a.Ref}, {a.Ref, "zz"}} {
		if _, err := s.AddDep(ctx, who, "key:dep", tc[0], tc[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("add %v = %v, want ErrNotFound", tc, err)
		}
	}
}

func TestSelfDependencyIsInvalidByRefOrULID(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a := mk(t, s, "aa", "a")
	for _, other := range []string{a.Ref, strings.ToUpper(a.Ref), a.ID} {
		_, err := s.AddDep(ctx, who, "key:dep", a.Ref, other)
		var inv InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &inv) || inv.Next == "" || !strings.Contains(err.Error(), "aa-1") {
			t.Errorf("self dependency on %q = %v", other, err)
		}
	}
	if depRows(t, s) != 0 || len(events(t, s, EventDepAdded)) != 0 {
		t.Error("a self dependency wrote something")
	}
}

func TestCyclesAreConflictsNamingThePath(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	for i := 0; i < 3; i++ {
		mk(t, s, "aa", "t")
	}
	add := func(a, b string) error { _, err := s.AddDep(ctx, who, "key:dep", a, b); return err }
	if err := add("aa-1", "aa-2"); err != nil {
		t.Fatal(err)
	}
	err := add("aa-2", "aa-1")
	var ce CycleError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &ce) || strings.Join(ce.Path, ">") != "aa-2>aa-1>aa-2" ||
		!strings.Contains(err.Error(), "aa-2 is blocked by aa-1 is blocked by aa-2") {
		t.Fatalf("2-cycle = %v", err)
	}
	if err := add("aa-2", "aa-3"); err != nil {
		t.Fatal(err)
	}
	err = add("aa-3", "aa-1")
	if !errors.Is(err, ErrConflict) || !errors.As(err, &ce) || strings.Join(ce.Path, ">") != "aa-3>aa-1>aa-2>aa-3" ||
		!strings.Contains(err.Error(), "aa-3 is blocked by aa-1 is blocked by aa-2 is blocked by aa-3") {
		t.Fatalf("3-cycle = %v", err)
	}
	if depRows(t, s) != 2 || len(events(t, s, EventDepAdded)) != 2 {
		t.Errorf("a refused cycle wrote something: rows %d", depRows(t, s))
	}
	// a dependency that closes no cycle is still accepted
	if err := add("aa-1", "aa-3"); err != nil {
		t.Errorf("diamond: %v", err)
	}
}

func TestDependencyAcrossTwoInitiativesIsAccepted(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	initiative(t, s, "bb")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "bb", "b")
	got, err := s.AddDep(ctx, who, "key:dep", a.Ref, b.Ref)
	if err != nil || !got.Blocked {
		t.Fatalf("cross-initiative add = %+v, %v", got, err)
	}
	ev := events(t, s, EventDepAdded)
	if len(ev) != 1 || ev[0].InitiativeID != a.InitiativeID || ev[0].Data["blocked_by"] != "bb-1" {
		t.Errorf("events = %+v", ev)
	}
	// the cycle search crosses initiatives too
	if _, err := s.AddDep(ctx, who, "key:dep", b.Ref, a.Ref); !errors.Is(err, ErrConflict) {
		t.Errorf("cross-initiative cycle = %v", err)
	}
}

func TestBlockedIsTrueWhileTheBlockerIsDoneNotClosedAndFalseOnceClosed(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	blocked := func() bool {
		t.Helper()
		got, err := s.Get(ctx, a.Ref)
		if err != nil {
			t.Fatal(err)
		}
		return got.Blocked
	}
	if blocked() {
		t.Fatal("blocked without a dependency")
	}
	if _, err := s.AddDep(ctx, who, "key:dep", a.Ref, b.Ref); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name string
		do   func() error
		want bool
	}{
		{"blocker open", func() error { return nil }, true},
		{"blocker done", func() error { _, err := s.Done(ctx, who, "key:a", b.Ref, DoneOptions{}); return err }, true},
		{"blocker closed", func() error { _, err := s.Close(ctx, who, "key:a", b.Ref, "completed"); return err }, false},
		{"blocker reopened", func() error { _, err := s.Reopen(ctx, who, "key:a", b.Ref); return err }, true},
	}
	for _, st := range steps {
		if err := st.do(); err != nil {
			t.Fatal(err)
		}
		if got := blocked(); got != st.want {
			t.Errorf("%s: blocked = %v, want %v", st.name, got, st.want)
		}
	}
}

func TestBlockedIgnoresAClosedBlockerWhenAnotherIsOpen(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b, c := mk(t, s, "aa", "a"), mk(t, s, "aa", "b"), mk(t, s, "aa", "c")
	for _, blocker := range []Task{b, c} {
		if _, err := s.AddDep(ctx, who, "key:dep", a.Ref, blocker.Ref); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Close(ctx, who, "key:a", b.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, a.Ref); !got.Blocked {
		t.Error("c still blocks")
	}
	if _, err := s.Close(ctx, who, "key:a", c.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, a.Ref); got.Blocked {
		t.Error("both blockers closed")
	}
}

func TestLinksListTheDependenciesAndTheBlockedTasksWithRefTitleAndStatus(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	initiative(t, s, "bb")
	a, b, c := mk(t, s, "aa", "first"), mk(t, s, "aa", "second"), mk(t, s, "bb", "third")
	for _, d := range [][2]string{{a.Ref, b.Ref}, {a.Ref, c.Ref}, {c.Ref, b.Ref}} {
		if _, err := s.AddDep(ctx, who, "key:dep", d[0], d[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Close(ctx, who, "key:dep", b.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Links(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	wantDeps := []Link{{"aa-2", "second", "closed", nil, nil}, {"bb-1", "third", "open", nil, nil}}
	if !reflect.DeepEqual(bareLinks(got.Deps), wantDeps) || got.Blocking == nil || len(got.Blocking) != 0 {
		t.Errorf("a: %#v", got)
	}
	got, err = s.Links(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	wantBlocking := []Link{{"aa-1", "first", "open", nil, nil}, {"bb-1", "third", "open", nil, nil}}
	if !reflect.DeepEqual(bareLinks(got.Blocking), wantBlocking) || got.Deps == nil || len(got.Deps) != 0 {
		t.Errorf("b: %#v", got)
	}
}

func TestCreateBlockedByStartsBlockedWithOneDepAddedEventPerBlocker(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	initiative(t, s, "bb")
	a, b := mk(t, s, "aa", "a"), mk(t, s, "bb", "b")
	got, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "c", BlockedBy: []string{"AA-1", b.ID, "bb-1", a.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocked || depRows(t, s) != 2 {
		t.Errorf("blocked %v, rows %d", got.Blocked, depRows(t, s))
	}
	ev := events(t, s, EventDepAdded)
	if len(ev) != 2 || ev[0].TaskID != got.ID || ev[0].Data["blocked_by"] != "aa-1" || ev[1].Data["blocked_by"] != "bb-1" ||
		ev[1].Data["ref"] != got.Ref {
		t.Errorf("events = %+v", ev)
	}
}

func TestCreateWithAnUnknownBlockerLeavesNothingBehind(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	mk(t, s, "aa", "a")
	for _, ref := range []string{"aa-9", "zz-1", "nonsense"} {
		_, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "c", BlockedBy: []string{"aa-1", ref}})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: err = %v", ref, err)
		}
	}
	if n := count(t, s, `SELECT COUNT(*) FROM tasks`); n != 1 || depRows(t, s) != 0 || len(events(t, s, EventDepAdded)) != 0 {
		t.Errorf("tasks %d deps %d", n, depRows(t, s))
	}
	again := mk(t, s, "aa", "after")
	if again.Seq != 2 {
		t.Errorf("a refused create used up a number: seq = %d", again.Seq)
	}
}

// bareLinks is the links without their tags (deps_test checks the tags of a link
// in its own test).
func bareLinks(list []Link) []Link {
	out := make([]Link, len(list))
	for i, l := range list {
		out[i] = Link{Ref: l.Ref, Title: l.Title, Status: l.Status}
	}
	return out
}
