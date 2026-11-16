package notify_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/notify"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

var (
	ctx   = context.Background()
	t0    = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	agent = audit.Actor{Handle: "claude@host"}
	other = audit.Actor{Handle: "codex@host"}
)

type env struct {
	st     *store.Store
	leases leases.Store
	tasks  tasks.Store
	asks   asks.Store
	clock  *time.Time
}

// tick moves the clock every store of the env reads.
func (e env) tick(d time.Duration) { *e.clock = e.clock.Add(d) }

func newEnv(t *testing.T, kinds []string) env {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if kinds != nil {
		st.SetNotify(kinds)
	}
	clock := new(time.Time)
	*clock = t0
	now := func() time.Time { return *clock }
	if _, err := (initiatives.Store{Store: st, Now: now}).Create(ctx, agent, "key:x", initiatives.New{Slug: "aa", Title: "I"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := (tasks.Store{Store: st, Now: now}).Create(ctx, agent, "key:x", tasks.New{Initiative: "aa", Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	return env{st: st, clock: clock, leases: leases.Store{Store: st, Now: now}, tasks: tasks.Store{Store: st, Now: now}, asks: asks.Store{Store: st, Now: now}}
}

// taskID is the id of task aa-<seq>.
func (e env) taskID(t *testing.T, ref string) string {
	t.Helper()
	var id string
	if err := e.st.Reader().QueryRowContext(ctx, e.st.Dialect().Rebind(`SELECT id FROM tasks WHERE seq = ?`), leaseN(t, ref[2:])).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// event appends an event of typ about the task ref (empty for none) and the row refID, and returns its id.
func (e env) event(t *testing.T, typ, taskRef, refID string) string {
	t.Helper()
	ev := audit.Event{Principal: "key:x", Type: typ, Summary: typ, RefID: refID}
	if taskRef != "" {
		ev.TaskID = e.taskID(t, taskRef)
	}
	var id string
	e.tx(t, func(tx *sql.Tx) error {
		var err error
		actorID, err := audit.UpsertActor(ctx, tx, e.st.Dialect(), agent, t0)
		if err != nil {
			return err
		}
		id, err = audit.AppendAsID(ctx, tx, e.st.Dialect(), actorID, t0, ev)
		return err
	})
	return id
}

func (e env) tx(t *testing.T, f func(*sql.Tx) error) {
	t.Helper()
	tx, err := e.st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func (e env) enqueue(t *testing.T, eventID, kind string) {
	t.Helper()
	e.tx(t, func(tx *sql.Tx) error { return notify.Enqueue(ctx, tx, e.st, eventID, kind, t0.Add(time.Minute)) })
}

func (e env) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.st.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueueWritesAPendingRowForAKindThatIsOn(t *testing.T) {
	e := newEnv(t, []string{store.NotifyDone})
	id := e.event(t, "task_done", "aa-1", "")
	e.enqueue(t, id, store.NotifyDone)
	var kind, eventID, payload, state string
	var attempts int
	var next, created int64
	var claimed, sent sql.NullInt64
	var lastErr sql.NullString
	err := e.st.Reader().QueryRowContext(ctx, `SELECT kind, event_id, payload, state, attempts, next_attempt_at, claimed_until, last_error, created_at, sent_at
		FROM notifications`).Scan(&kind, &eventID, &payload, &state, &attempts, &next, &claimed, &lastErr, &created, &sent)
	if err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Minute).UnixMilli()
	if kind != "done" || eventID != id || !strings.HasPrefix(payload, `{"text":"done: aa-1 t (claude@host)","kind":"done"`) || state != "pending" || attempts != 0 ||
		next != at || created != at || claimed.Valid || lastErr.Valid || sent.Valid {
		t.Fatalf("row = %s %s %s %s %d %d %d %v %v %v", kind, eventID, payload, state, attempts, next, created, claimed, lastErr, sent)
	}
}

func TestEnqueueWritesNothingForAKindThatIsOffOrWhenAlertsAreOff(t *testing.T) {
	for name, kinds := range map[string][]string{"kind not listed": {store.NotifyAsking}, "alerts off": nil} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, kinds)
			e.enqueue(t, e.event(t, "task_done", "aa-1", ""), store.NotifyDone)
			if n := e.count(t); n != 0 {
				t.Fatalf("%d rows", n)
			}
		})
	}
}

func TestEnqueueRefusesASecondRowForOneEventAndKind(t *testing.T) {
	e := newEnv(t, []string{store.NotifyAsking})
	if _, err := e.asks.Raise(ctx, agent, "key:x", asks.New{Task: "aa-1", Question: "which?"}); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := e.st.Reader().QueryRowContext(ctx, `SELECT event_id FROM notifications`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tx, err := e.st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := notify.Enqueue(ctx, tx, e.st, id, store.NotifyAsking, t0); err == nil {
		t.Fatal("a second asking row for the event was written")
	}
}

func TestEnqueueWritesOneRowPerKindForOneEvent(t *testing.T) {
	e := newEnv(t, []string{store.NotifyDone, store.NotifyAsking})
	if _, err := e.asks.Raise(ctx, agent, "key:x", asks.New{Task: "aa-1", Question: "which?"}); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := e.st.Reader().QueryRowContext(ctx, `SELECT event_id FROM notifications`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	e.enqueue(t, id, store.NotifyDone)
	if n := e.count(t); n != 2 {
		t.Fatalf("%d rows, want one per kind", n)
	}
}

// leaseOn starts a lease on aa-1 or aa-2 and returns its id.
func (e env) leaseOn(t *testing.T, a audit.Actor, task string) string {
	t.Helper()
	r, err := e.leases.Start(ctx, a, "key:x", leases.Start{Task: task, Intent: "work"})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := e.st.Reader().QueryRowContext(ctx, e.st.Dialect().Rebind(`SELECT id FROM leases WHERE n = ?`), leaseN(t, r.Lease.Ref)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func leaseN(t *testing.T, ref string) int {
	t.Helper()
	n, err := strconv.Atoi(ref[1:])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// taskRefOf is the ref of the task with the id.
func (e env) taskRefOf(t *testing.T, id string) string {
	t.Helper()
	var seq int
	if err := e.st.Reader().QueryRowContext(ctx, e.st.Dialect().Rebind(`SELECT seq FROM tasks WHERE id = ?`), id).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return "aa-" + strconv.Itoa(seq)
}

func (e env) stranded(t *testing.T, leaseID string) {
	t.Helper()
	var taskID string
	if err := e.st.Reader().QueryRowContext(ctx, e.st.Dialect().Rebind(`SELECT task_id FROM leases WHERE id = ?`), leaseID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	ev := e.event(t, "lease_expired", e.taskRefOf(t, taskID), leaseID)
	e.tx(t, func(tx *sql.Tx) error { return notify.EnqueueStranded(ctx, tx, e.st, ev, leaseID, t0) })
}

func (e env) setStarted(t *testing.T, leaseID string, at time.Time) {
	t.Helper()
	if _, err := e.st.Writer().ExecContext(ctx, e.st.Dialect().Rebind(`UPDATE leases SET started_at = ? WHERE id = ?`), at.UnixMilli(), leaseID); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueStrandedForTheOnlyLeaseOfATask(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.stranded(t, e.leaseOn(t, agent, "aa-1"))
	if n := e.count(t); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}

func TestEnqueueStrandedJudgesNewestByStartedAtThenID(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	first, second := e.leaseOn(t, agent, "aa-1"), e.leaseOn(t, other, "aa-1")

	// the same started_at: the larger id is the newest
	e.setStarted(t, first, t0)
	e.setStarted(t, second, t0)
	e.stranded(t, first)
	if n := e.count(t); n != 0 {
		t.Fatalf("the lease with the smaller id enqueued %d rows on a started_at tie", n)
	}
	e.stranded(t, second)
	if n := e.count(t); n != 1 {
		t.Fatalf("the lease with the larger id enqueued %d rows in total, want 1", n)
	}

	// started_at outranks id: the first lease, started later, is now the newest
	e.setStarted(t, first, t0.Add(time.Hour))
	e.stranded(t, second)
	if n := e.count(t); n != 1 {
		t.Fatalf("the lease that started earlier enqueued, %d rows in total", n)
	}
	e.stranded(t, first)
	if n := e.count(t); n != 2 {
		t.Fatalf("the lease that started later enqueued %d rows in total, want 2", n)
	}
}

func TestEnqueueStrandedIgnoresLeasesOfOtherTasks(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	older := e.leaseOn(t, agent, "aa-1")
	e.leaseOn(t, agent, "aa-2")
	e.setStarted(t, older, t0.Add(-time.Hour))
	e.stranded(t, older)
	if n := e.count(t); n != 1 {
		t.Fatalf("%d rows: a lease on another task made this one not the newest", n)
	}
}

func TestEnqueueStrandedWritesNothingWhenStrandedIsOff(t *testing.T) {
	e := newEnv(t, []string{store.NotifyDone})
	e.stranded(t, e.leaseOn(t, agent, "aa-1"))
	if n := e.count(t); n != 0 {
		t.Fatalf("%d rows", n)
	}
}
