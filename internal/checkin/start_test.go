package checkin

import (
	"errors"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// rows is the tasks, leases, events and actors counts, which a refused start
// must leave as they were.
func (e env) rows(t *testing.T) string {
	t.Helper()
	var got string
	err := e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM tasks) || '/' || (SELECT count(*) FROM leases) || '/' ||
		(SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM actors)`).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (e env) start(t *testing.T, in StartRequest) StartResult {
	t.Helper()
	r, err := e.S.Start(ctx, me, "k", in)
	if err != nil {
		t.Fatalf("start %+v: %v", in, err)
	}
	return r
}

func TestAStartOnAnExistingTaskWritesTheLeaseAndOneEventAndChecksIn(t *testing.T) {
	e := newEnv(t)
	before := e.rows(t)
	r := e.start(t, StartRequest{Task: "aa-1", Intent: "work", Resources: []string{"db:x"}})
	if r.Lease.Lease.Ref != "L1" || r.Lease.Renewed || r.TaskCreated || r.ProtocolSeen != 0 {
		t.Errorf("start = %+v", r)
	}
	if len(r.Now.Leases.Mine) != 1 || r.Now.Leases.Mine[0].Ref != "L1" {
		t.Errorf("the check-in holds %d of my leases, want the new one", len(r.Now.Leases.Mine))
	}
	if got, want := e.rows(t), "2/1/4/1"; got == before || got != want {
		t.Errorf("rows = %s (were %s), want %s", got, before, want)
	}
}

func TestAStartAgainRenewsTheLiveLease(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "first"})
	e.advance(time.Minute)
	r := e.start(t, StartRequest{Task: "aa-1", Intent: "second"})
	if !r.Lease.Renewed || r.Lease.Lease.Ref != "L1" || r.Lease.Lease.Intent != "second" || r.TaskCreated {
		t.Errorf("second start = %+v", r)
	}
	if got := e.rows(t); got != "2/1/5/1" {
		t.Errorf("rows = %s, want one lease and one renewal event more", got)
	}
}

func TestAStartThatSharesAResourceWritesTheLeaseAndListsTheConflict(t *testing.T) {
	e := newEnv(t)
	if _, err := e.S.Leases.Start(ctx, bob, "k", leases.Start{Task: "aa-2", Intent: "theirs", Resources: []string{"db:x"}}); err != nil {
		t.Fatal(err)
	}
	r := e.start(t, StartRequest{Task: "aa-1", Intent: "mine", Resources: []string{"db:x"}})
	if len(r.Lease.Conflicts) != 1 || r.Lease.Conflicts[0].Holder != bob.Handle || r.Lease.Lease.Ref != "L2" {
		t.Errorf("conflicts = %+v on %s", r.Lease.Conflicts, r.Lease.Lease.Ref)
	}
	if len(r.Now.Leases.Conflicts) != 1 {
		t.Errorf("the check-in lists %d conflicts, want 1", len(r.Now.Leases.Conflicts))
	}
}

func TestAStartOnADoneClosedOrUnknownTaskWritesNothing(t *testing.T) {
	e := newEnv(t)
	if _, err := (tasks.Store{Store: e.S.Leases.Store, Now: func() time.Time { return *e.Now }}).Close(ctx, me, "k", "aa-2", "dropped"); err != nil {
		t.Fatal(err)
	}
	before := e.rows(t)
	for task, want := range map[string]error{"aa-2": leases.ErrConflict, "aa-9": leases.ErrNotFound} {
		_, err := e.S.Start(ctx, bob, "k", StartRequest{Task: task, Intent: "x"})
		if !errors.Is(err, want) {
			t.Errorf("start on %s = %v, want %v", task, err, want)
		}
	}
	if got := e.rows(t); got != before {
		t.Errorf("rows = %s after refusals, were %s", got, before)
	}
}

func TestAStartOnANewTaskCreatesTheTaskAndTheLeaseTogether(t *testing.T) {
	e := newEnv(t)
	r := e.start(t, StartRequest{NewTask: &NewTask{Initiative: "aa", Title: "fresh"}, Intent: "doing it"})
	if !r.TaskCreated || r.Lease.Lease.Task != "aa-3" || r.Lease.Renewed {
		t.Errorf("start = %+v", r)
	}
	if got := e.rows(t); got != "3/1/5/1" {
		t.Errorf("rows = %s, want one task, one lease and two events more", got)
	}
}

func TestAStartOnANewTaskThatFailsLeavesNoTask(t *testing.T) {
	e := newEnv(t)
	if _, err := (initiatives.Store{Store: e.S.Leases.Store, Now: func() time.Time { return *e.Now }}).Create(ctx, me, "k", initiatives.New{Slug: "bb", Title: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (tasks.Store{Store: e.S.Leases.Store, Now: func() time.Time { return *e.Now }}).CloseInitiative(ctx, me, "k", "bb", false); err != nil {
		t.Fatal(err)
	}
	before := e.rows(t)
	cases := []struct {
		name string
		in   StartRequest
		want error
	}{
		{"closed initiative", StartRequest{NewTask: &NewTask{Initiative: "bb", Title: "t"}, Intent: "x"}, tasks.ErrInitiativeClosed},
		{"unknown initiative", StartRequest{NewTask: &NewTask{Initiative: "zz", Title: "t"}, Intent: "x"}, tasks.ErrNoInitiative},
		{"empty title", StartRequest{NewTask: &NewTask{Initiative: "aa", Title: ""}, Intent: "x"}, tasks.ErrInvalid},
		{"bad intent after the task is made", StartRequest{NewTask: &NewTask{Initiative: "aa", Title: "t"}, Intent: ""}, leases.ErrInvalid},
	}
	for _, c := range cases {
		if _, err := e.S.Start(ctx, bob, "k", c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if got := e.rows(t); got != before {
		t.Errorf("rows = %s after refusals, were %s", got, before)
	}
}

func TestAStartNeedsExactlyOneOfTaskAndNewTask(t *testing.T) {
	e := newEnv(t)
	before := e.rows(t)
	for name, in := range map[string]StartRequest{
		"neither": {Intent: "x"},
		"both":    {Task: "aa-1", NewTask: &NewTask{Initiative: "aa", Title: "t"}, Intent: "x"},
	} {
		_, err := e.S.Start(ctx, me, "k", in)
		var bad leases.InvalidError
		if !errors.As(err, &bad) || bad.Next == "" {
			t.Errorf("%s: err = %v, want an InvalidError with a next", name, err)
		}
	}
	if got := e.rows(t); got != before {
		t.Errorf("rows = %s, were %s", got, before)
	}
}

func TestAStartAnswersTheProtocolTheActorHasSeen(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "x"})
	if _, err := e.S.Leases.Store.Writer().ExecContext(ctx, `UPDATE actors SET protocol_seen = 4 WHERE handle = ?`, me.Handle); err != nil {
		t.Fatal(err)
	}
	if r := e.start(t, StartRequest{Task: "aa-1", Intent: "again"}); r.ProtocolSeen != 4 {
		t.Errorf("protocol_seen = %d, want 4", r.ProtocolSeen)
	}
	if r := e.start(t, StartRequest{Task: "aa-2", Intent: "second task"}); r.ProtocolSeen != 4 {
		t.Errorf("second task of the same actor: protocol_seen = %d", r.ProtocolSeen)
	}
	r, err := e.S.Start(ctx, bob, "k", StartRequest{Task: "aa-2", Intent: "bob"})
	if err != nil || r.ProtocolSeen != 0 {
		t.Errorf("an actor that has seen none: protocol_seen = %d, err %v", r.ProtocolSeen, err)
	}
}
