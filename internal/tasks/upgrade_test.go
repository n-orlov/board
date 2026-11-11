package tasks

import (
	"testing"
	"time"

	"github.com/n-orlov/board/internal/leases"
)

// forceDone writes a task straight to done, the way a Phase 2 database came to
// hold a done task with a live lease.
func forceDone(t *testing.T, s Store, ref string) {
	t.Helper()
	if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE tasks SET status = 'done', resolution = NULL, done_at = 5, done_by = (SELECT id FROM actors LIMIT 1) WHERE seq = ? AND initiative_id = (SELECT id FROM initiatives WHERE slug = 'aa')`, int(ref[len(ref)-1]-'0')); err != nil {
		t.Fatal(err)
	}
}

func TestReopenLeasedReopensADoneTaskWithALiveLeaseAsItsOldestHolder(t *testing.T) {
	s, ls, tk := leased(t)
	first := startLease(t, ls, who, tk.Ref, "work")
	other := startLease(t, ls, bobby, tk.Ref, "also")
	forceDone(t, s, tk.Ref)
	n, err := s.ReopenLeased(ctx)
	if err != nil || n != 1 {
		t.Fatalf("ReopenLeased = %d, %v, want 1", n, err)
	}
	got, err := s.Get(ctx, tk.Ref)
	if err != nil || got.Status != "open" || got.DoneAt != nil || got.DoneBy != "" {
		t.Fatalf("task = %+v, %v, want open with done_at and done_by cleared", got, err)
	}
	ev := events(t, s, EventReopened)
	if len(ev) != 1 {
		t.Fatalf("task_reopened events = %+v, want exactly one", ev)
	}
	e := ev[0]
	if e.Principal != "system:upgrade" || e.TaskID != tk.ID || e.InitiativeID != tk.InitiativeID || e.RefID != tk.ID ||
		e.Data["reason"] != "live lease at upgrade" || e.Data["ref"] != tk.Ref || e.Data["from"] != "done" || e.Data["lease"] != first.Ref {
		t.Errorf("event = %+v, want system:upgrade, reason, ref, from done and lease %s", e, first.Ref)
	}
	if e.Summary != "task "+tk.Ref+" reopened" {
		t.Errorf("summary = %q", e.Summary)
	}
	if by := eventActor(t, s, e.TaskID); by != who.Handle {
		t.Errorf("event actor = %q, want the oldest live lease's holder %q", by, who.Handle)
	}
	for _, l := range []string{first.Ref, other.Ref} {
		if st := leaseNow(t, ls, l).State; st != "live" {
			t.Errorf("lease %s is %s, want live", l, st)
		}
	}
	if n, err := s.ReopenLeased(ctx); err != nil || n != 0 {
		t.Fatalf("second ReopenLeased = %d, %v, want 0", n, err)
	}
	if len(events(t, s, EventReopened)) != 1 {
		t.Error("a second run appended an event")
	}
}

func eventActor(t *testing.T, s Store, taskID string) string {
	t.Helper()
	var h string
	err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(
		`SELECT a.handle FROM events e JOIN actors a ON a.id = e.actor_id WHERE e.task_id = ? AND e.type = 'task_reopened'`), taskID).Scan(&h)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestReopenLeasedLeavesEveryOtherTaskAlone(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	ls := leases.Store{Store: s.Store, Now: s.Now}
	done := mk(t, s, "aa", "done without a lease")
	open := mk(t, s, "aa", "open with a lease")
	closed := mk(t, s, "aa", "closed")
	startLease(t, ls, who, open.Ref, "work")
	if _, err := s.Done(ctx, who, "key:p", done.Ref, DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(ctx, who, "key:p", closed.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ReopenLeased(ctx); err != nil || n != 0 {
		t.Fatalf("ReopenLeased = %d, %v, want 0", n, err)
	}
	for ref, want := range map[string]string{done.Ref: "done", open.Ref: "open", closed.Ref: "closed"} {
		if got, _ := s.Get(ctx, ref); got.Status != want {
			t.Errorf("%s is %s, want %s", ref, got.Status, want)
		}
	}
	if len(events(t, s, EventReopened)) != 0 {
		t.Error("an event was appended")
	}
}

func TestReopenLeasedIgnoresEndedAndExpiredLeases(t *testing.T) {
	s, ls, tk := leased(t)
	l := startLease(t, ls, who, tk.Ref, "work")
	forceDone(t, s, tk.Ref)
	late := Store{Store: s.Store, Now: func() time.Time { return l.ExpiresAt.Add(time.Second) }}
	if n, err := late.ReopenLeased(ctx); err != nil || n != 0 {
		t.Fatalf("ReopenLeased after the lease ran out = %d, %v, want 0", n, err)
	}
	if got, _ := s.Get(ctx, tk.Ref); got.Status != "done" {
		t.Errorf("task is %s, want done", got.Status)
	}
	if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE leases SET state = 'ended'`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ReopenLeased(ctx); err != nil || n != 0 {
		t.Fatalf("ReopenLeased with the lease ended = %d, %v, want 0", n, err)
	}
}

func TestReopenLeasedReopensEachTaskOnce(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	ls := leases.Store{Store: s.Store, Now: s.Now}
	a, b := mk(t, s, "aa", "a"), mk(t, s, "aa", "b")
	startLease(t, ls, who, a.Ref, "x")
	startLease(t, ls, bobby, b.Ref, "y")
	startLease(t, ls, bobby, a.Ref, "z")
	forceDone(t, s, a.Ref)
	forceDone(t, s, b.Ref)
	if n, err := s.ReopenLeased(ctx); err != nil || n != 2 {
		t.Fatalf("ReopenLeased = %d, %v, want 2", n, err)
	}
	if got := len(events(t, s, EventReopened)); got != 2 {
		t.Errorf("task_reopened events = %d, want 2", got)
	}
}
