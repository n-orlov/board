package tasks

import (
	"errors"
	"github.com/n-orlov/board/internal/audit"
	"strings"
	"testing"
	"time"
)

type rowState struct {
	Status, Resolution, DoneBy, ClosedBy string
	DoneAt, ClosedAt                     bool
}

func audit2(handle string) audit.Actor { return audit.Actor{Handle: handle} }

func state(tk Task) rowState {
	return rowState{tk.Status, tk.Resolution, tk.DoneBy, tk.ClosedBy, tk.DoneAt != nil, tk.ClosedAt != nil}
}

// must returns a function that fails the test on an error and else the task.
func must(t *testing.T) func(Task, error) Task {
	return func(tk Task, err error) Task {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
}

func TestDoneSetsDoneFieldsAndWritesOneTaskDoneEvent(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "one")
	other := audit2("codex@host")
	got, err := s.Done(ctx, other, "key:done", tk.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if want := (rowState{Status: "done", DoneBy: "codex@host", DoneAt: true}); state(got) != want {
		t.Fatalf("state = %+v, want %+v", state(got), want)
	}
	if !got.UpdatedAt.After(tk.UpdatedAt) || !got.DoneAt.Equal(got.UpdatedAt) {
		t.Errorf("done_at %v updated_at %v created %v", got.DoneAt, got.UpdatedAt, tk.UpdatedAt)
	}
	ev := events(t, s, EventDone)
	if len(ev) != 1 || ev[0].TaskID != tk.ID || ev[0].RefID != tk.ID || ev[0].InitiativeID != tk.InitiativeID ||
		ev[0].Principal != "key:done" || ev[0].Data["ref"] != "aa-1" || ev[0].Data["from"] != "open" || !strings.Contains(ev[0].Summary, "aa-1") {
		t.Errorf("events = %+v", ev)
	}
}

func TestReopenFromDoneAndFromClosedClearsTheDoneAndClosedFields(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	for _, tc := range []struct {
		name string
		to   func(Task) error
		from string
	}{
		{"done", func(tk Task) error { _, err := s.Done(ctx, who, "key:a", tk.Ref); return err }, "done"},
		{"closed", func(tk Task) error { _, err := s.Close(ctx, who, "key:a", tk.Ref, "dropped"); return err }, "closed"},
	} {
		tk := mk(t, s, "aa", tc.name)
		if err := tc.to(tk); err != nil {
			t.Fatal(err)
		}
		got, err := s.Reopen(ctx, audit2("codex@host"), "key:re", tk.Ref)
		if err != nil {
			t.Fatalf("reopen from %s: %v", tc.name, err)
		}
		if state(got) != (rowState{Status: "open"}) || got.DoneAt != nil || got.ClosedAt != nil {
			t.Errorf("reopen from %s left %+v", tc.name, got)
		}
		again, err := s.Get(ctx, tk.Ref)
		if err != nil || state(again) != (rowState{Status: "open"}) {
			t.Errorf("stored task after reopen from %s = %+v, %v", tc.name, again, err)
		}
		var found bool
		for _, e := range events(t, s, EventReopened) {
			found = found || (e.TaskID == tk.ID && e.Principal == "key:re" && e.Data["from"] == tc.from)
		}
		if !found {
			t.Errorf("no task_reopened event from %s", tc.name)
		}
	}
	if n := len(events(t, s, EventReopened)); n != 2 {
		t.Errorf("task_reopened events = %d, want 2", n)
	}
}

func TestCloseFromOpenAndFromDoneRecordsTheResolution(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	open := mk(t, s, "aa", "open")
	done := mk(t, s, "aa", "done")
	d := must(t)(s.Done(ctx, who, "key:a", done.Ref))
	got, err := s.Close(ctx, audit2("codex@host"), "key:c", open.Ref, "superseded")
	if err != nil {
		t.Fatal(err)
	}
	if want := (rowState{Status: "closed", Resolution: "superseded", ClosedBy: "codex@host", ClosedAt: true}); state(got) != want {
		t.Errorf("close from open = %+v, want %+v", state(got), want)
	}
	got, err = s.Close(ctx, who, "key:c", done.Ref, "completed")
	if err != nil {
		t.Fatal(err)
	}
	if want := (rowState{Status: "closed", Resolution: "completed", ClosedBy: who.Handle, ClosedAt: true, DoneBy: who.Handle, DoneAt: true}); state(got) != want {
		t.Errorf("close from done = %+v, want %+v", state(got), want)
	}
	if !got.DoneAt.Equal(*d.DoneAt) {
		t.Errorf("done_at moved from %v to %v", d.DoneAt, got.DoneAt)
	}
	ev := events(t, s, EventClosed)
	if len(ev) != 2 || ev[0].TaskID != open.ID || ev[0].Data["resolution"] != "superseded" || ev[0].Data["from"] != "open" ||
		ev[1].TaskID != done.ID || ev[1].Data["resolution"] != "completed" || ev[1].Data["from"] != "done" ||
		!strings.Contains(ev[0].Summary, "superseded") || ev[0].Principal != "key:c" {
		t.Errorf("events = %+v", ev)
	}
}

func TestCloseAcceptsTheFourResolutions(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	for _, r := range []string{"completed", "dropped", "superseded", "duplicate"} {
		tk := mk(t, s, "aa", r)
		got, err := s.Close(ctx, who, "key:a", tk.Ref, r)
		if err != nil || got.Status != "closed" || got.Resolution != r {
			t.Errorf("close as %s = %+v, %v", r, got, err)
		}
	}
	if n := len(events(t, s, EventClosed)); n != 4 {
		t.Errorf("task_closed events = %d, want 4", n)
	}
}

func TestCloseRefusesAMissingOrUnknownResolutionAsInvalid(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "x")
	before := snapshot(t, s)
	for _, r := range []string{"", "wontfix", "Completed", " dropped"} {
		_, err := s.Close(ctx, who, "key:a", tk.Ref, r)
		var inv InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &inv) || inv.Next == "" {
			t.Errorf("resolution %q: err = %v, want InvalidError", r, err)
		}
	}
	if got := snapshot(t, s); got != before {
		t.Errorf("refused resolutions changed %+v to %+v", before, got)
	}
	if g, _ := s.Get(ctx, tk.Ref); g.Status != "open" || g.Resolution != "" {
		t.Errorf("task = %+v", g)
	}
}

type counts struct{ events, actors, updated int }

func snapshot(t *testing.T, s Store) counts {
	t.Helper()
	return counts{count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM actors`),
		count(t, s, `SELECT count(*) FROM tasks WHERE updated_at <> created_at`)}
}

func TestRefusedTransitionsAreConflictsNamingTheStatusAndWriteNothing(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	open := mk(t, s, "aa", "open")
	done := must(t)(s.Done(ctx, who, "key:a", mk(t, s, "aa", "done").Ref))
	closed := mk(t, s, "aa", "closed")
	closed = must(t)(s.Close(ctx, who, "key:a", closed.Ref, "dropped"))
	newcomer := audit2("never-seen@host")
	for _, tc := range []struct {
		name string
		task Task
		call func(ref string) (Task, error)
		want string
	}{
		{"done on done", done, func(ref string) (Task, error) { return s.Done(ctx, newcomer, "key:n", ref) }, "done"},
		{"done on closed", closed, func(ref string) (Task, error) { return s.Done(ctx, newcomer, "key:n", ref) }, "closed"},
		{"reopen on open", open, func(ref string) (Task, error) { return s.Reopen(ctx, newcomer, "key:n", ref) }, "open"},
		{"close on closed", closed, func(ref string) (Task, error) { return s.Close(ctx, newcomer, "key:n", ref, "completed") }, "closed"},
	} {
		before := snapshot(t, s)
		_, err := tc.call(tc.task.Ref)
		var ce ConflictError
		if !errors.Is(err, ErrConflict) || !errors.As(err, &ce) || ce.Status != tc.want || !strings.Contains(err.Error(), "is "+tc.want) || !strings.Contains(err.Error(), tc.task.Ref) {
			t.Errorf("%s: err = %v, want a conflict naming %s", tc.name, err, tc.want)
		}
		if got := snapshot(t, s); got != before {
			t.Errorf("%s wrote something: %+v -> %+v", tc.name, before, got)
		}
		after, _ := s.Get(ctx, tc.task.Ref)
		if state(after) != state(tc.task) || !after.UpdatedAt.Equal(tc.task.UpdatedAt) {
			t.Errorf("%s changed the task: %+v -> %+v", tc.name, tc.task, after)
		}
	}
}

func TestTransitionsRefuseAnUnknownTask(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	for name, f := range map[string]func() error{
		"done":   func() error { _, err := s.Done(ctx, who, "k", "aa-9"); return err },
		"reopen": func() error { _, err := s.Reopen(ctx, who, "k", "aa"); return err },
		"close":  func() error { _, err := s.Close(ctx, who, "k", "aa-9", "dropped"); return err },
	} {
		if err := f(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
	}
}

func TestReopenInAClosedInitiativeIsRefusedAndWritesNothing(t *testing.T) {
	s := newStore(t)
	ini := initiative(t, s, "aa")
	tk := mk(t, s, "aa", "x")
	tk = must(t)(s.Close(ctx, who, "key:a", tk.Ref, "dropped"))
	if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE initiatives SET status = 'closed' WHERE id = ?`, ini.ID); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)
	if _, err := s.Reopen(ctx, who, "key:a", tk.Ref); !errors.Is(err, ErrInitiativeClosed) {
		t.Errorf("reopen = %v, want ErrInitiativeClosed", err)
	}
	if got := snapshot(t, s); got != before {
		t.Errorf("changed %+v to %+v", before, got)
	}
	if g, _ := s.Get(ctx, tk.Ref); g.Status != "closed" {
		t.Errorf("status = %s", g.Status)
	}
}

func TestTransitionsDoNotTouchTheEditableFields(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	end := t0.Add(time.Hour)
	tk, err := s.Create(ctx, who, "k", New{Initiative: "aa", Title: "keep", Description: "d", PlannedEnd: &end})
	if err != nil {
		t.Fatal(err)
	}
	got := must(t)(s.Done(ctx, who, "k", tk.Ref))
	got = must(t)(s.Reopen(ctx, who, "k", got.Ref))
	if got.Title != "keep" || got.Description != "d" || got.PlannedEnd == nil || !got.PlannedEnd.Equal(end) || got.Seq != tk.Seq {
		t.Errorf("fields changed: %+v", got)
	}
}

func onBehalfOf(t *testing.T, s Store, typ string) []string {
	t.Helper()
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT COALESCE(ob.handle, '') FROM events e LEFT JOIN actors ob ON ob.id = e.on_behalf_of WHERE e.type = ? ORDER BY e.n`), typ)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		out = append(out, h)
	}
	return out
}

func TestOnlyCloseRecordsTheActorTheRequestIsMadeFor(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	a, b, c := mk(t, s, "aa", "a"), mk(t, s, "aa", "b"), mk(t, s, "aa", "c")
	behalf := audit.Actor{Handle: "claude@host", OnBehalfOf: "operator"}
	must(t)(s.Done(ctx, behalf, "key:a", a.Ref))
	must(t)(s.Close(ctx, behalf, "key:a", b.Ref, "dropped"))
	must(t)(s.Close(ctx, who, "key:a", c.Ref, "dropped"))
	must(t)(s.Reopen(ctx, behalf, "key:a", c.Ref))
	if got := onBehalfOf(t, s, EventClosed); len(got) != 2 || got[0] != "operator" || got[1] != "" {
		t.Errorf("task_closed on behalf of %q, want [operator \"\"]", got)
	}
	for _, typ := range []string{EventDone, EventReopened} {
		if got := onBehalfOf(t, s, typ); len(got) != 1 || got[0] != "" {
			t.Errorf("%s on behalf of %q, want none", typ, got)
		}
	}
}

func TestRefusedCloseRecordsNoActorForTheHeader(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "a")
	behalf := audit.Actor{Handle: "claude@host", OnBehalfOf: "operator"}
	if _, err := s.Close(ctx, behalf, "key:a", tk.Ref, "wontfix"); err == nil {
		t.Fatal("close with an unknown resolution succeeded")
	}
	must(t)(s.Close(ctx, who, "key:a", tk.Ref, "dropped"))
	if _, err := s.Close(ctx, behalf, "key:a", tk.Ref, "dropped"); err == nil {
		t.Fatal("close of a closed task succeeded")
	}
	var n int
	if err := s.Store.Reader().QueryRowContext(ctx, `SELECT count(*) FROM actors WHERE handle = 'operator'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("operator rows = %d (%v), want 0", n, err)
	}
}
