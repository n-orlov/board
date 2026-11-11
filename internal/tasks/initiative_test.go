package tasks

import (
	"errors"
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/initiatives"
)

func typesOf(t *testing.T, s Store) []string {
	t.Helper()
	rows, err := s.Store.Reader().QueryContext(ctx, `SELECT type FROM events WHERE type IN ('task_closed', 'initiative_closed') ORDER BY n`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var typ string
		if err := rows.Scan(&typ); err != nil {
			t.Fatal(err)
		}
		out = append(out, typ)
	}
	return out
}

func TestCloseInitiativeWithNoTasksClosesItWithOneEvent(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	got, err := s.CloseInitiative(ctx, audit2("op@host"), "key:c", "AA", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "closed" || got.ClosedAt == nil || got.ClosedBy != "op@host" {
		t.Fatalf("closed = %+v", got)
	}
	ev := events(t, s, initiatives.EventClosed)
	if len(ev) != 1 || ev[0].InitiativeID != got.ID || ev[0].RefID != got.ID || ev[0].Principal != "key:c" ||
		ev[0].Data["slug"] != "aa" || ev[0].Data["force"] != false || ev[0].Data["dropped"] != float64(0) {
		t.Errorf("events = %+v", ev)
	}
}

func TestCloseInitiativeRefusesATaskThatIsNotClosedWithoutForce(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	mk(t, s, "aa", "open one")
	done := mk(t, s, "aa", "done one")
	must(t)(s.Done(ctx, who, "key:a", done.Ref, DoneOptions{}))
	before := count(t, s, `SELECT count(*) FROM events`)
	_, err := s.CloseInitiative(ctx, who, "key:c", "aa", false)
	var c initiatives.ConflictError
	if !errors.Is(err, initiatives.ErrConflict) || !errors.As(err, &c) ||
		!strings.Contains(c.Msg, "2 of its tasks") || !strings.Contains(c.Next, "force") {
		t.Fatalf("err = %v", err)
	}
	after := count(t, s, `SELECT count(*) FROM events`)
	if before != after || count(t, s, `SELECT count(*) FROM initiatives WHERE status = 'closed'`) != 0 {
		t.Errorf("a refusal wrote: events %d -> %d", before, after)
	}
	if got := must(t)(s.Get(ctx, "aa-1")); got.Status != "open" {
		t.Errorf("task = %s", got.Status)
	}
}

func TestCloseInitiativeWithForceDropsTheTasksThenClosesIt(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	mk(t, s, "aa", "open one")
	done := mk(t, s, "aa", "done one")
	must(t)(s.Done(ctx, who, "key:a", done.Ref, DoneOptions{}))
	shut := mk(t, s, "aa", "closed one")
	must(t)(s.Close(ctx, who, "key:a", shut.Ref, "completed"))
	mk(t, s, "aa", "fourth")
	initiative(t, s, "bb")
	mk(t, s, "bb", "elsewhere")

	got, err := s.CloseInitiative(ctx, who, "key:c", "aa", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "closed" {
		t.Fatalf("status = %s", got.Status)
	}
	// aa-3 was closed before the initiative: its event comes first.
	if types := typesOf(t, s); strings.Join(types, ",") != "task_closed,task_closed,task_closed,task_closed,initiative_closed" {
		t.Errorf("event types = %v", types)
	}
	for ref, resolution := range map[string]string{"aa-1": "dropped", "aa-2": "dropped", "aa-3": "completed", "aa-4": "dropped", "bb-1": ""} {
		task := must(t)(s.Get(ctx, ref))
		if task.Resolution != resolution || (resolution != "" && task.Status != "closed") || (resolution == "" && task.Status != "open") {
			t.Errorf("%s = %s %q, want resolution %q", ref, task.Status, task.Resolution, resolution)
		}
	}
	ev := events(t, s, initiatives.EventClosed)
	if len(ev) != 1 || ev[0].Data["force"] != true || ev[0].Data["dropped"] != float64(3) {
		t.Errorf("event = %+v", ev)
	}
	if first := events(t, s, EventClosed); len(first) != 4 || first[2].Data["from"] != "done" {
		t.Errorf("task events = %+v", first)
	}
}

func TestCloseInitiativeOfAClosedOrUnknownOneIsRefused(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	if _, err := s.CloseInitiative(ctx, who, "key:c", "aa", false); err != nil {
		t.Fatal(err)
	}
	_, err := s.CloseInitiative(ctx, who, "key:c", "aa", true)
	var c initiatives.ConflictError
	if !errors.As(err, &c) || !strings.Contains(c.Msg, "already closed") || !strings.Contains(c.Next, "reopen") {
		t.Fatalf("close closed = %v", err)
	}
	if n := len(events(t, s, initiatives.EventClosed)); n != 1 {
		t.Errorf("%d initiative_closed events", n)
	}
	if _, err := s.CloseInitiative(ctx, who, "key:c", "zz", false); !errors.Is(err, initiatives.ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
}

func TestATaskOfAClosedInitiativeCannotBeCreatedPatchedOrReopened(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "one")
	must(t)(s.Close(ctx, who, "key:a", tk.Ref, "completed"))
	live := mk(t, s, "aa", "two")
	if _, err := s.CloseInitiative(ctx, who, "key:c", "aa", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, who, "key:a", New{Initiative: "aa", Title: "new"}); !errors.Is(err, ErrInitiativeClosed) {
		t.Errorf("create = %v", err)
	}
	if _, err := s.Reopen(ctx, who, "key:a", tk.Ref); !errors.Is(err, ErrInitiativeClosed) {
		t.Errorf("reopen = %v", err)
	}
	if _, err := s.Reopen(ctx, who, "key:a", live.Ref); !errors.Is(err, ErrInitiativeClosed) {
		t.Errorf("reopen dropped = %v", err)
	}
}
