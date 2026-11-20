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

// event appends an event of typ about task aa-1 and returns its id.
func (e env) event(t *testing.T, typ string) string {
	t.Helper()
	ev := audit.Event{Principal: "key:x", Type: typ, Summary: typ, TaskID: e.taskID(t, "aa-1")}
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
	id := e.event(t, "task_done")
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
			e.enqueue(t, e.event(t, "task_done"), store.NotifyDone)
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

// expiredEvents makes n lease_expired events and returns the Stranded of each,
// the newest flag true for every second one.
func (e env) expiredEvents(t *testing.T, n int) []notify.Stranded {
	t.Helper()
	var list []notify.Stranded
	e.tx(t, func(tx *sql.Tx) error {
		actorID, err := audit.UpsertActor(ctx, tx, e.st.Dialect(), agent, t0)
		if err != nil {
			return err
		}
		events := make([]audit.Written, n)
		for i := range events {
			events[i] = audit.Written{ActorID: actorID, Event: audit.Event{Principal: "system:expiry", Type: "lease_expired", Summary: "L" + strconv.Itoa(i)}}
		}
		ids, err := audit.AppendBatch(ctx, tx, e.st.Dialect(), t0, events)
		for i, id := range ids {
			list = append(list, notify.Stranded{EventID: id, Newest: i%2 == 0, Holder: notify.Actor{Handle: "claude@host"},
				LeaseRef: "L" + strconv.Itoa(i), Intent: "work", ExpiredAt: t0, TaskRef: "aa-1", TaskTitle: "t"})
		}
		return err
	})
	return list
}

func (e env) enqueueBatch(t *testing.T, list []notify.Stranded) {
	t.Helper()
	e.tx(t, func(tx *sql.Tx) error { return notify.EnqueueStrandedBatch(ctx, tx, e.st, list, t0.Add(time.Minute)) })
}

func TestEnqueueStrandedBatchWritesARowOnlyForTheNewestLeaseOfEachTask(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.enqueueBatch(t, e.expiredEvents(t, 5))
	if n := e.count(t); n != 3 {
		t.Fatalf("%d rows for 5 events of which 3 are the newest lease of their task", n)
	}
}

func TestEnqueueStrandedBatchWritesEveryRowPastOneStatementsWorth(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.enqueueBatch(t, e.expiredEvents(t, 2301))
	if n := e.count(t); n != 1151 {
		t.Fatalf("%d rows for 2301 events, want the 1151 newest", n)
	}
}

func TestEnqueueStrandedBatchWritesNothingForNoEventsOrWhenStrandedIsOff(t *testing.T) {
	e := newEnv(t, []string{store.NotifyDone})
	e.enqueueBatch(t, e.expiredEvents(t, 3))
	e.enqueueBatch(t, nil)
	if n := e.count(t); n != 0 {
		t.Fatalf("%d rows", n)
	}
	on := newEnv(t, store.NotifyKinds)
	on.enqueueBatch(t, nil)
	if n := on.count(t); n != 0 {
		t.Fatalf("%d rows for no events", n)
	}
}

func TestEnqueueStrandedBatchRefusesASecondRowForOneEvent(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	list := e.expiredEvents(t, 1)
	e.enqueueBatch(t, list)
	tx, err := e.st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := notify.EnqueueStrandedBatch(ctx, tx, e.st, list, t0); err == nil {
		t.Fatal("a second stranded row for the event was written")
	}
}

// TestAStrandedPayloadIsTheDocumentBuildPayloadReads proves the payload a batch
// writes from what the expiry read is the one BuildPayload reads back from the
// database for the same event, with a public URL and without one.
func TestAStrandedPayloadIsTheDocumentBuildPayloadReads(t *testing.T) {
	for name, url := range map[string]string{"no public url": "", "public url": "https://board.example.com/"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, store.NotifyKinds)
			e.st.SetPublicURL(url)
			e.leaseOn(t, audit.Actor{Handle: "claude@host", WayBack: "tmux 4"}, "aa-1")
			e.tick(48 * time.Hour)
			if n, err := e.leases.ExpireDue(ctx); err != nil || n != 1 {
				t.Fatalf("ExpireDue = %d, %v", n, err)
			}
			var eventID, stored string
			if err := e.st.Reader().QueryRowContext(ctx, `SELECT event_id, payload FROM notifications`).Scan(&eventID, &stored); err != nil {
				t.Fatal(err)
			}
			var built []byte
			e.tx(t, func(tx *sql.Tx) error {
				var err error
				built, err = notify.BuildPayload(ctx, tx, e.st, eventID, store.NotifyStranded, url)
				return err
			})
			if stored != string(built) {
				t.Fatalf("batch payload\n%s\nBuildPayload\n%s", stored, built)
			}
		})
	}
}
