package tasks

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/leases"
)

var bobby = audit.Actor{Handle: "codex@host"}

// leased is a store with a task aa-1, so the
// ids of the live leases are L1, L2, ... in start order.
func leased(t *testing.T) (Store, leases.Store, Task) {
	t.Helper()
	s := newStore(t)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "one")
	return s, leases.Store{Store: s.Store, Now: s.Now}, tk
}

func startLease(t *testing.T, ls leases.Store, a audit.Actor, ref, intent string) leases.Lease {
	t.Helper()
	r, err := ls.Start(ctx, a, "key:"+a.Handle, leases.Start{Task: ref, Intent: intent})
	if err != nil {
		t.Fatal(err)
	}
	return r.Lease
}

func leaseNow(t *testing.T, ls leases.Store, ref string) leases.Lease {
	t.Helper()
	l, err := ls.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestDoneByTheHolderEndsItsLeaseWithTheOutcomeGivenOrDone(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{"given": {"  shipped it ", "shipped it"}, "default": {"", "done"}, "blank": {"   ", "done"}} {
		t.Run(name, func(t *testing.T) {
			s, ls, tk := leased(t)
			l := startLease(t, ls, who, tk.Ref, "work")
			got, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{Outcome: tc.in})
			if err != nil || got.Status != "done" {
				t.Fatalf("done = %+v, %v", got, err)
			}
			end := leaseNow(t, ls, l.Ref)
			if end.State != "ended" || end.Outcome != tc.want || end.EndedBy != "" || end.EndedAt == nil {
				t.Errorf("lease = %+v, want ended with outcome %q and no ended_by", end, tc.want)
			}
			ev := events(t, s, leases.EventEnded)
			if len(ev) != 1 || ev[0].Principal != "key:p" || ev[0].TaskID != tk.ID || ev[0].RefID != l.ID ||
				ev[0].Data["outcome"] != tc.want || ev[0].Data["ref"] != l.Ref || ev[0].Data["task"] != tk.Ref || ev[0].Data["ended_by"] != nil {
				t.Errorf("events = %+v", ev)
			}
			if n := len(events(t, s, EventDone)); n != 1 {
				t.Errorf("task_done events = %d", n)
			}
		})
	}
}

func TestDoneWithAnotherActorsLiveLeaseIsRefusedNamingItAndChangesNothing(t *testing.T) {
	s, ls, tk := leased(t)
	mine := startLease(t, ls, who, tk.Ref, "mine")
	theirs := startLease(t, ls, bobby, tk.Ref, "refactor the parser")
	_, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{Outcome: "x"})
	var held LeaseHeldError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &held) {
		t.Fatalf("err = %v, want a LeaseHeldError", err)
	}
	if len(held.Holders) != 1 || held.Holders[0] != (LeaseHeld{Ref: theirs.Ref, Holder: "codex@host", Intent: "refactor the parser"}) {
		t.Errorf("holders = %+v", held.Holders)
	}
	for _, want := range []string{tk.Ref, "codex@host", theirs.Ref, "refactor the parser"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if held.Next() != "wait for "+theirs.Ref+" to end, or --force" {
		t.Errorf("next = %q", held.Next())
	}
	if got, _ := s.Get(ctx, tk.Ref); got.Status != "open" || got.DoneAt != nil {
		t.Errorf("task = %+v, want it still open", got)
	}
	if a, b := leaseNow(t, ls, mine.Ref), leaseNow(t, ls, theirs.Ref); a.State != "live" || b.State != "live" {
		t.Errorf("leases = %s, %s, want both live", a.State, b.State)
	}
	if n := len(events(t, s, EventDone)) + len(events(t, s, leases.EventEnded)); n != 0 {
		t.Errorf("a refused done wrote %d events", n)
	}
}

func TestHeldErrorNamesEveryHolder(t *testing.T) {
	e := LeaseHeldError{Ref: "aa-1", Holders: []LeaseHeld{{"L1", "a@h", "one"}, {"L2", "b@h", "two"}}}
	if got := e.Error(); got != "cannot mark task aa-1 done: a@h holds lease L1 (one); b@h holds lease L2 (two)" {
		t.Errorf("error = %q", got)
	}
	if e.Next() != "wait for L1 to end, or --force" {
		t.Errorf("next = %q", e.Next())
	}
}

func TestForcedDoneEndsEveryLiveLeaseAndMarksTheTaskDone(t *testing.T) {
	s, ls, tk := leased(t)
	mine := startLease(t, ls, who, tk.Ref, "mine")
	theirs := startLease(t, ls, bobby, tk.Ref, "theirs")
	got, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{Outcome: "shipped", Force: true})
	if err != nil || got.Status != "done" {
		t.Fatalf("done = %+v, %v", got, err)
	}
	for _, l := range []leases.Lease{leaseNow(t, ls, mine.Ref), leaseNow(t, ls, theirs.Ref)} {
		if l.State != "ended" || l.Outcome != "ended: task marked done by claude@host" || l.EndedBy != "claude@host" {
			t.Errorf("lease %s = %+v, want ended by claude@host with the forced outcome", l.Ref, l)
		}
	}
	ev := events(t, s, leases.EventEnded)
	if len(ev) != 2 || ev[0].RefID != mine.ID || ev[1].RefID != theirs.ID {
		t.Fatalf("events = %+v", ev)
	}
	for _, e := range ev {
		if e.Data["ended_by"] != "claude@host" || e.Data["outcome"] != "ended: task marked done by claude@host" {
			t.Errorf("event data = %v", e.Data)
		}
	}
}

func TestForcedDoneByAnActorWithNoLeaseOfItsOwn(t *testing.T) {
	s, ls, tk := leased(t)
	theirs := startLease(t, ls, bobby, tk.Ref, "theirs")
	if _, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if b := leaseNow(t, ls, theirs.Ref); b.State != "ended" || b.EndedBy != "claude@host" || b.Outcome != "ended: task marked done by claude@host" {
		t.Errorf("lease = %+v", b)
	}
}

func TestDoneIgnoresALeasePastItsExpiryAndExpiresIt(t *testing.T) {
	s, ls, tk := leased(t)
	ttl := int64(1)
	r, err := ls.Start(ctx, bobby, "key:b", leases.Start{Task: tk.Ref, Intent: "x", TTL: &ttl})
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return t0.Add(time.Hour) }
	ls.Now = s.Now
	if _, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{}); err != nil {
		t.Fatalf("done with an expired lease of another: %v", err)
	}
	if l := leaseNow(t, ls, r.Lease.Ref); l.State != "expired" {
		t.Errorf("lease = %+v, want expired, not ended", l)
	}
	if n := len(events(t, s, leases.EventExpired)); n != 1 {
		t.Errorf("lease_expired events = %d", n)
	}
}

func TestDoneRefusesALongOutcomeBeforeReadingTheTask(t *testing.T) {
	s, _, tk := leased(t)
	_, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{Outcome: strings.Repeat("x", 201)})
	var bad InvalidError
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || bad.Next == "" {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Done(ctx, who, "key:p", "aa-9", DoneOptions{Outcome: strings.Repeat("x", 201)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown task with a long outcome: %v, want ErrInvalid first", err)
	}
	if got, _ := s.Get(ctx, tk.Ref); got.Status != "open" {
		t.Errorf("status = %s", got.Status)
	}
}

func TestCloseEndsEveryLiveLeaseOnTheTask(t *testing.T) {
	s, ls, tk := leased(t)
	mine := startLease(t, ls, who, tk.Ref, "mine")
	theirs := startLease(t, ls, bobby, tk.Ref, "theirs")
	other := mk(t, s, "aa", "two")
	elsewhere := startLease(t, ls, bobby, other.Ref, "elsewhere")
	if _, err := s.Close(ctx, who, "key:p", tk.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	for _, l := range []leases.Lease{leaseNow(t, ls, mine.Ref), leaseNow(t, ls, theirs.Ref)} {
		if l.State != "ended" || l.Outcome != "ended: task closed by claude@host" || l.EndedBy != "claude@host" {
			t.Errorf("lease %s = %+v, want ended by claude@host with the close outcome", l.Ref, l)
		}
	}
	if l := leaseNow(t, ls, elsewhere.Ref); l.State != "live" {
		t.Errorf("a lease on another task = %+v, want live", l)
	}
	if n := len(events(t, s, leases.EventEnded)); n != 2 {
		t.Errorf("lease_ended events = %d", n)
	}
}

func TestCloseOfADoneTaskAndOfATaskWithNoLeaseEndsNothing(t *testing.T) {
	s, _, tk := leased(t)
	if _, err := s.Done(ctx, who, "key:p", tk.Ref, DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(ctx, who, "key:p", tk.Ref, "completed"); err != nil {
		t.Fatal(err)
	}
	if n := len(events(t, s, leases.EventEnded)); n != 0 {
		t.Errorf("lease_ended events = %d", n)
	}
}

// A task is never done or closed while it has a live lease: after done or close
// settles, no lease on the task is live and a start against it is refused with
// a conflict that changes nothing, whichever transition settled it.
func TestAStartAfterDoneOrCloseIsRefusedAndLeavesNoLiveLease(t *testing.T) {
	for name, settle := range map[string]func(Store, string) error{
		"done": func(s Store, ref string) error {
			_, err := s.Done(ctx, who, "key:p", ref, DoneOptions{})
			return err
		},
		"closed": func(s Store, ref string) error {
			_, err := s.Close(ctx, who, "key:p", ref, "dropped")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, ls, tk := leased(t)
			startLease(t, ls, who, tk.Ref, "work")
			if err := settle(s, tk.Ref); err != nil {
				t.Fatal(err)
			}
			started := len(events(t, s, leases.EventStarted))
			_, err := ls.Start(ctx, bobby, "key:b", leases.Start{Task: tk.Ref, Intent: "late"})
			var ce leases.ConflictError
			if !errors.As(err, &ce) {
				t.Fatalf("start after %s = %v, want a conflict", name, err)
			}
			live, lerr := ls.LiveOf(ctx, tk.ID)
			if lerr != nil || len(live) != 0 {
				t.Errorf("live leases after %s = %+v, %v, want none", name, live, lerr)
			}
			if n := len(events(t, s, leases.EventStarted)); n != started {
				t.Errorf("lease_started events = %d, want %d: a refused start writes nothing", n, started)
			}
		})
	}
}

// A forced done or a close settles leases held by several actors at once; none
// of them survives live, and each transition leaves exactly one ended event per
// lease it ended.
func TestDoneForcedAndCloseLeaveNoLiveLeaseHeldByAnyActor(t *testing.T) {
	for name, settle := range map[string]func(Store, string) error{
		"forced done": func(s Store, ref string) error {
			_, err := s.Done(ctx, who, "key:p", ref, DoneOptions{Force: true})
			return err
		},
		"close": func(s Store, ref string) error {
			_, err := s.Close(ctx, who, "key:p", ref, "dropped")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, ls, tk := leased(t)
			startLease(t, ls, who, tk.Ref, "mine")
			startLease(t, ls, bobby, tk.Ref, "theirs")
			if err := settle(s, tk.Ref); err != nil {
				t.Fatal(err)
			}
			live, err := ls.LiveOf(ctx, tk.ID)
			if err != nil || len(live) != 0 {
				t.Errorf("live leases after %s = %+v, %v, want none", name, live, err)
			}
			if n := len(events(t, s, leases.EventEnded)); n != 2 {
				t.Errorf("lease_ended events = %d, want 2", n)
			}
		})
	}
}
