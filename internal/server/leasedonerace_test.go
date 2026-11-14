package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/tasks"
)

// raceRig is one database seen through independent stores, the way separate
// board processes on one Postgres see it: one for each side of a race and one
// that only reads and holds the gate.
type raceRig struct {
	leg                          string
	gate                         *store.Store
	startSide, transitSide       *store.Store
	leasesStart, leasesTransit   leases.Store
	tasksStart, tasksTransit     tasks.Store
	initiativeRef                string
	owner, other, closer, nobody audit.Actor
	seq                          int
}

func newRaceRig(t *testing.T, leg string, dbs databases) *raceRig {
	t.Helper()
	url := dbs.New(t)
	gate := openStore(t, url)
	if err := gate.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := &raceRig{leg: leg, gate: gate, startSide: gate, transitSide: gate, initiativeRef: "rc",
		owner: audit.Actor{Handle: "owner@host"}, other: audit.Actor{Handle: "other@host"},
		closer: audit.Actor{Handle: "closer@host"}, nobody: audit.Actor{Handle: "late@host"}}
	if leg == "postgres" {
		r.startSide, r.transitSide = openStore(t, url), openStore(t, url)
	}
	r.leasesStart, r.leasesTransit = leases.Store{Store: r.startSide}, leases.Store{Store: r.transitSide}
	r.tasksStart, r.tasksTransit = tasks.Store{Store: r.startSide}, tasks.Store{Store: r.transitSide}
	if _, err := (initiatives.Store{Store: gate}).Create(context.Background(), r.owner, "key:o", initiatives.New{Slug: "rc", Title: "Race"}); err != nil {
		t.Fatal(err)
	}
	return r
}

// task creates an open task and returns its ref.
func (r *raceRig) task(t *testing.T) string {
	t.Helper()
	r.seq++
	tk, err := (tasks.Store{Store: r.gate}).Create(context.Background(), r.owner, "key:o", tasks.New{Initiative: r.initiativeRef, Title: fmt.Sprintf("task %d", r.seq)})
	if err != nil {
		t.Fatal(err)
	}
	return tk.Ref
}

// blockedBackends counts the sessions of this database waiting on a lock.
func blockedBackends(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n)
	return n, err
}

// ordered runs first and then second, with first's transaction certain to take
// the task's lock before second's. On Postgres both calls are in flight at
// once: a transaction of the test holds the task row, each call is started in
// turn and waits until it is blocked on that row (so it has read the task), and
// only then is the row released, so the calls run through the database in the
// order they queued. On SQLite one writer serialises them, and the calls run
// one after the other.
func (r *raceRig) ordered(t *testing.T, ref string, first, second func()) {
	t.Helper()
	if r.leg != "postgres" {
		first()
		second()
		return
	}
	hold, err := r.gate.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := hold.QueryRowContext(context.Background(), `SELECT t.id FROM tasks t JOIN initiatives i ON i.id = t.initiative_id WHERE i.slug = 'rc' AND t.seq = $1`, seqOf(t, ref)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := hold.ExecContext(context.Background(), `SELECT id FROM tasks WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i, call := range []func(){first, second} {
		wg.Add(1)
		go func() { defer wg.Done(); call() }()
		deadline := time.Now().Add(20 * time.Second)
		for {
			n, err := blockedBackends(r.gate.Reader())
			if err != nil {
				t.Fatal(err)
			}
			if n == i+1 {
				break
			}
			if time.Now().After(deadline) {
				_ = hold.Rollback()
				wg.Wait()
				t.Fatalf("call %d did not wait for the task's row lock (%d sessions blocked): it does not take the task lock", i+1, n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func seqOf(t *testing.T, ref string) int {
	t.Helper()
	var seq int
	if _, err := fmt.Sscanf(ref, "rc-%d", &seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// count runs a query that returns one number.
func (r *raceRig) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := r.gate.Reader().QueryRowContext(context.Background(), r.gate.Dialect().Rebind(query), args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// invariant fails when any done or closed task holds a live lease.
func (r *raceRig) invariant(t *testing.T) {
	t.Helper()
	if n := r.count(t, `SELECT count(*) FROM leases l JOIN tasks t ON t.id = l.task_id WHERE l.state = 'live' AND t.status IN ('done', 'closed')`); n != 0 {
		t.Fatalf("%d live leases on done or closed tasks", n)
	}
}

func (r *raceRig) status(t *testing.T, ref string) string {
	t.Helper()
	tk, err := (tasks.Store{Store: r.gate}).Get(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return tk.Status
}

func (r *raceRig) leasesOn(t *testing.T, ref string, state string) int {
	t.Helper()
	return r.count(t, `SELECT count(*) FROM leases l JOIN tasks t ON t.id = l.task_id JOIN initiatives i ON i.id = t.initiative_id
		WHERE i.slug = 'rc' AND t.seq = ? AND l.state = ?`, seqOf(t, ref), state)
}

// transitions are the two ways a task leaves open that must end its leases.
var raceTransitions = []struct {
	name string
	run  func(ctx context.Context, s tasks.Store, a audit.Actor, ref string) error
	want string
}{
	{"done", func(ctx context.Context, s tasks.Store, a audit.Actor, ref string) error {
		_, err := s.Done(ctx, a, "key:t", ref, tasks.DoneOptions{})
		return err
	}, "done"},
	{"close", func(ctx context.Context, s tasks.Store, a audit.Actor, ref string) error {
		_, err := s.Close(ctx, a, "key:t", ref, "completed")
		return err
	}, "closed"},
}

// A start that queued first wins: the done or close that follows ends the
// lease the start wrote, so the committed task holds no live lease.
func TestStartThenDoneOrCloseEndsTheLeaseTheStartWrote(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		for _, tr := range raceTransitions {
			t.Run(tr.name, func(t *testing.T) {
				r := newRaceRig(t, leg, dbs)
				ref := r.task(t)
				var startErr, trErr error
				r.ordered(t, ref, func() {
					_, startErr = r.leasesStart.Start(context.Background(), r.owner, "key:s", leases.Start{Task: ref, Intent: "working"})
				}, func() { trErr = tr.run(context.Background(), r.tasksTransit, r.owner, ref) })
				if startErr != nil || trErr != nil {
					t.Fatalf("start: %v, %s: %v", startErr, tr.name, trErr)
				}
				if got := r.status(t, ref); got != tr.want {
					t.Errorf("status = %s, want %s", got, tr.want)
				}
				if n := r.leasesOn(t, ref, "ended"); n != 1 {
					t.Errorf("ended leases = %d, want the one the start wrote", n)
				}
				r.invariant(t)
			})
		}
	})
}

// A start that queued behind a done or close is refused with a conflict and
// writes no lease, no event and no actor.
func TestDoneOrCloseThenStartIsRefusedAndWritesNothing(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		for _, tr := range raceTransitions {
			t.Run(tr.name, func(t *testing.T) {
				r := newRaceRig(t, leg, dbs)
				ref := r.task(t)
				var startErr, trErr error
				r.ordered(t, ref, func() { trErr = tr.run(context.Background(), r.tasksTransit, r.closer, ref) }, func() {
					_, startErr = r.leasesStart.Start(context.Background(), r.nobody, "key:s", leases.Start{Task: ref, Intent: "too late"})
				})
				var conflict leases.ConflictError
				if trErr != nil || !errors.As(startErr, &conflict) || !errors.Is(startErr, leases.ErrConflict) {
					t.Fatalf("%s: %v, start: %v (want a ConflictError)", tr.name, trErr, startErr)
				}
				if got := r.status(t, ref); got != tr.want {
					t.Errorf("status = %s, want %s", got, tr.want)
				}
				if n := r.count(t, `SELECT count(*) FROM leases`); n != 0 {
					t.Errorf("leases = %d, want none", n)
				}
				if n := r.count(t, `SELECT count(*) FROM events WHERE type IN ('lease_started', 'lease_renewed', 'lease_ended')`); n != 0 {
					t.Errorf("lease events = %d, want none", n)
				}
				if n := r.count(t, `SELECT count(*) FROM actors WHERE handle = ?`, r.nobody.Handle); n != 0 {
					t.Errorf("the refused starter was written as an actor")
				}
				r.invariant(t)
			})
		}
	})
}

// The existing rules apply to a transition that follows a winning start: a done
// by another actor is refused while the lease lives, and a forced done or a close
// ends it.
func TestTransitionAfterAWinningStartKeepsTheHolderForceAndCloseRules(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		t.Run("done by another is refused", func(t *testing.T) {
			r := newRaceRig(t, leg, dbs)
			ref := r.task(t)
			var doneErr error
			r.ordered(t, ref, func() {
				_, _ = r.leasesStart.Start(context.Background(), r.owner, "key:s", leases.Start{Task: ref, Intent: "mine"})
			}, func() {
				_, doneErr = r.tasksTransit.Done(context.Background(), r.other, "key:t", ref, tasks.DoneOptions{})
			})
			var held tasks.LeaseHeldError
			if !errors.As(doneErr, &held) || len(held.Holders) != 1 {
				t.Fatalf("done = %v, want LeaseHeldError", doneErr)
			}
			if got := r.status(t, ref); got != "open" || r.leasesOn(t, ref, "live") != 1 {
				t.Errorf("status %s, live leases %d: a refused done must change nothing", got, r.leasesOn(t, ref, "live"))
			}
			r.invariant(t)
		})
		t.Run("forced done ends it", func(t *testing.T) {
			r := newRaceRig(t, leg, dbs)
			ref := r.task(t)
			var doneErr error
			r.ordered(t, ref, func() {
				_, _ = r.leasesStart.Start(context.Background(), r.owner, "key:s", leases.Start{Task: ref, Intent: "mine"})
			}, func() {
				_, doneErr = r.tasksTransit.Done(context.Background(), r.other, "key:t", ref, tasks.DoneOptions{Force: true})
			})
			if doneErr != nil || r.status(t, ref) != "done" || r.leasesOn(t, ref, "live") != 0 {
				t.Fatalf("forced done: %v, status %s", doneErr, r.status(t, ref))
			}
			r.invariant(t)
		})
		t.Run("close of the initiative ends it", func(t *testing.T) {
			r := newRaceRig(t, leg, dbs)
			ref := r.task(t)
			var closeErr error
			r.ordered(t, ref, func() {
				_, _ = r.leasesStart.Start(context.Background(), r.owner, "key:s", leases.Start{Task: ref, Intent: "mine"})
			}, func() {
				_, closeErr = r.tasksTransit.CloseInitiative(context.Background(), r.other, "key:t", "rc", true)
			})
			if closeErr != nil || r.status(t, ref) != "closed" {
				t.Fatalf("close initiative: %v, status %s", closeErr, r.status(t, ref))
			}
			r.invariant(t)
		})
	})
}

// A start behind the close of its task's initiative (which closes every task)
// is refused, and a renew behind a close finds its lease ended instead of
// extending it.
func TestStartAndRenewBehindACloseSeeTheClosedTask(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		t.Run("start behind initiative close", func(t *testing.T) {
			r := newRaceRig(t, leg, dbs)
			ref := r.task(t)
			var startErr error
			r.ordered(t, ref, func() {
				_, _ = r.tasksTransit.CloseInitiative(context.Background(), r.closer, "key:t", "rc", true)
			}, func() {
				_, startErr = r.leasesStart.Start(context.Background(), r.nobody, "key:s", leases.Start{Task: ref, Intent: "late"})
			})
			if !errors.Is(startErr, leases.ErrConflict) {
				t.Fatalf("start = %v, want a conflict", startErr)
			}
			if n := r.count(t, `SELECT count(*) FROM leases`); n != 0 {
				t.Errorf("leases = %d, want none", n)
			}
			r.invariant(t)
		})
		t.Run("renew behind close", func(t *testing.T) {
			r := newRaceRig(t, leg, dbs)
			ref := r.task(t)
			res, err := r.leasesStart.Start(context.Background(), r.owner, "key:s", leases.Start{Task: ref, Intent: "mine"})
			if err != nil {
				t.Fatal(err)
			}
			var renewErr error
			r.ordered(t, ref, func() {
				_, _ = r.tasksTransit.Close(context.Background(), r.closer, "key:t", ref, "dropped")
			}, func() {
				_, renewErr = r.leasesStart.Renew(context.Background(), r.owner, "key:s", res.Lease.Ref, leases.Renew{})
			})
			if !errors.Is(renewErr, leases.ErrConflict) {
				t.Fatalf("renew = %v, want a conflict (the lease ended with the close)", renewErr)
			}
			if n := r.count(t, `SELECT count(*) FROM events WHERE type = 'lease_renewed'`); n != 0 {
				t.Errorf("lease_renewed events = %d, want none", n)
			}
			r.invariant(t)
		})
	})
}

// Many starts race many dones and closes on independent stores: whichever
// commits first, no committed done or closed task holds a live lease.
func TestRacingStartsAndTransitionsNeverLeaveALiveLeaseOnAFinishedTask(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		r := newRaceRig(t, leg, dbs)
		const tasksN = 24
		refs := make([]string, tasksN)
		for i := range refs {
			refs[i] = r.task(t)
		}
		var wg sync.WaitGroup
		for i, ref := range refs {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, _ = r.leasesStart.Start(context.Background(), r.owner, "key:s", leases.Start{Task: ref, Intent: "racing"})
			}()
			go func() {
				defer wg.Done()
				tr := raceTransitions[i%len(raceTransitions)]
				_ = tr.run(context.Background(), r.tasksTransit, r.owner, ref)
			}()
		}
		wg.Wait()
		for _, ref := range refs {
			if got := r.status(t, ref); got == "open" {
				t.Errorf("%s still open after its transition", ref)
			}
		}
		r.invariant(t)
	})
}
