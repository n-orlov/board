package server

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/notes"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// outboxWorld is a migrated database with one initiative "aa" and open tasks
// aa-1 to aa-3, every store reading one hand-moved clock, and the alerts
// configured as the test asks.
type outboxWorld struct {
	t      *testing.T
	url    string
	st     *store.Store
	now    time.Time
	tasks  tasks.Store
	leases leases.Store
	asks   asks.Store
	notes  notes.Store
}

var (
	outboxAgent = audit.Actor{Handle: "claude@host"}
	outboxOther = audit.Actor{Handle: "codex@host"}
	outboxStart = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
)

// newOutboxWorld opens a database of the leg. kinds is what BOARD_NOTIFY_ON
// allows once BOARD_NOTIFY_URL is set; a nil kinds leaves the URL unset.
func newOutboxWorld(t *testing.T, dbs databases, kinds []string) *outboxWorld {
	t.Helper()
	url := dbs.New(t)
	st := openStore(t, url)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if kinds != nil {
		st.SetNotify(kinds)
	}
	w := &outboxWorld{t: t, url: url, st: st, now: outboxStart}
	clock := func() time.Time { return w.now }
	w.tasks, w.leases = tasks.Store{Store: st, Now: clock}, leases.Store{Store: st, Now: clock}
	w.asks, w.notes = asks.Store{Store: st, Now: clock}, notes.Store{Store: st, Now: clock}
	ctx := context.Background()
	if _, err := (initiatives.Store{Store: st, Now: clock}).Create(ctx, outboxAgent, "key:x", initiatives.New{Slug: "aa", Title: "I"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w.tick(time.Second)
		if _, err := w.tasks.Create(ctx, outboxAgent, "key:x", tasks.New{Initiative: "aa", Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w *outboxWorld) tick(d time.Duration) { w.now = w.now.Add(d) }

func (w *outboxWorld) lease(actor audit.Actor, task string, ttl int64) leases.Result {
	w.t.Helper()
	w.tick(time.Second)
	r, err := w.leases.Start(context.Background(), actor, "key:"+actor.Handle, leases.Start{Task: task, Intent: "work on " + task, TTL: &ttl})
	if err != nil {
		w.t.Fatalf("start a lease on %s: %v", task, err)
	}
	return r
}

func (w *outboxWorld) ask(in asks.New) asks.Ask {
	w.t.Helper()
	w.tick(time.Second)
	k, err := w.asks.Raise(context.Background(), outboxAgent, "key:x", in)
	if err != nil {
		w.t.Fatalf("raise %+v: %v", in, err)
	}
	return k
}

// rows is the outbox as "kind of event type" strings, in insertion order.
func (w *outboxWorld) rows() []string {
	w.t.Helper()
	rs, err := w.st.Reader().QueryContext(context.Background(), `SELECT n.kind, e.type, n.state, n.attempts, n.next_attempt_at, n.created_at, n.event_id
		FROM notifications n JOIN events e ON e.id = n.event_id ORDER BY e.n`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	var out []string
	for rs.Next() {
		var kind, typ, state, eventID string
		var attempts int
		var next, created int64
		if err := rs.Scan(&kind, &typ, &state, &attempts, &next, &created, &eventID); err != nil {
			w.t.Fatal(err)
		}
		if state != "pending" || attempts != 0 || next != created || eventID == "" {
			w.t.Fatalf("row %s/%s = state %s attempts %d next %d created %d", kind, typ, state, attempts, next, created)
		}
		out = append(out, kind+" of "+typ)
	}
	if err := rs.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func (w *outboxWorld) wantRows(want ...string) {
	w.t.Helper()
	got := w.rows()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		w.t.Fatalf("outbox = %q, want %q", got, want)
	}
}

func allKinds() []string { return append([]string(nil), store.NotifyKinds...) }

func TestEachTriggerEnqueuesOneNotification(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		ctx := context.Background()
		w := newOutboxWorld(t, dbs, allKinds())

		w.lease(outboxAgent, "aa-1", 60)
		w.wantRows()
		w.tick(2 * time.Minute)
		if n, err := w.leases.ExpireDue(ctx); err != nil || n != 1 {
			t.Fatalf("ExpireDue = %d, %v", n, err)
		}
		w.wantRows("stranded of lease_expired")
		if n, err := w.leases.ExpireDue(ctx); err != nil || n != 0 {
			t.Fatalf("second ExpireDue = %d, %v", n, err)
		}
		w.wantRows("stranded of lease_expired")

		w.ask(asks.New{Task: "aa-1", Question: "on a task?"})
		w.ask(asks.New{Initiative: "aa", Question: "on an initiative?"})
		w.ask(asks.New{Question: "on the board?"})
		w.wantRows("stranded of lease_expired", "asking of ask_raised", "asking of ask_raised", "asking of ask_raised")

		w.tick(time.Second)
		if _, err := w.tasks.Done(ctx, outboxAgent, "key:x", "aa-2", tasks.DoneOptions{}); err != nil {
			t.Fatal(err)
		}
		w.wantRows("stranded of lease_expired", "asking of ask_raised", "asking of ask_raised", "asking of ask_raised", "done of task_done")
	})
}

func TestDoneThatEndsTheHoldersLeaseEnqueuesOneDoneRow(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newOutboxWorld(t, dbs, allKinds())
		w.lease(outboxAgent, "aa-1", 600)
		w.tick(time.Second)
		if _, err := w.tasks.Done(context.Background(), outboxAgent, "key:x", "aa-1", tasks.DoneOptions{Outcome: "shipped"}); err != nil {
			t.Fatal(err)
		}
		var ended int
		if err := w.st.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE type = 'lease_ended'`).Scan(&ended); err != nil || ended != 1 {
			t.Fatalf("lease_ended events = %d, %v: the done request should have ended the holder's lease", ended, err)
		}
		w.wantRows("done of task_done")
	})
}

func TestOnlyTheNewestExpiredLeaseOfATaskEnqueues(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newOutboxWorld(t, dbs, allKinds())
		w.lease(outboxAgent, "aa-1", 60)
		w.lease(outboxOther, "aa-1", 120) // started later, so it is the newest
		w.lease(outboxAgent, "aa-2", 60)
		w.tick(10 * time.Minute)
		if n, err := w.leases.ExpireDue(context.Background()); err != nil || n != 3 {
			t.Fatalf("ExpireDue = %d, %v, want all three leases", n, err)
		}
		w.wantRows("stranded of lease_expired", "stranded of lease_expired")
		var tasksOf []string
		rs, err := w.st.Reader().QueryContext(context.Background(), `SELECT e.ref_id FROM notifications n JOIN events e ON e.id = n.event_id ORDER BY e.n`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var id string
			if err := rs.Scan(&id); err != nil {
				t.Fatal(err)
			}
			tasksOf = append(tasksOf, w.leaseTask(id))
		}
		if !reflect.DeepEqual(tasksOf, []string{"aa-2", "aa-1"}) {
			t.Fatalf("notified leases are on %q, want aa-2 and aa-1 (its newest lease), in expiry order", tasksOf)
		}
		if got := w.expiredEvents(); got != 3 {
			t.Fatalf("lease_expired events = %d, want 3: every expiry keeps its event", got)
		}
	})
}

// leaseTask is the ref of the task the lease row id is on.
func (w *outboxWorld) leaseTask(id string) string {
	w.t.Helper()
	var slug string
	var seq int
	err := w.st.Reader().QueryRowContext(context.Background(), w.st.Dialect().Rebind(`SELECT i.slug, t.seq FROM leases l JOIN tasks t ON t.id = l.task_id
		JOIN initiatives i ON i.id = t.initiative_id WHERE l.id = ?`), id).Scan(&slug, &seq)
	if err != nil {
		w.t.Fatal(err)
	}
	return fmt.Sprintf("%s-%d", slug, seq)
}

func (w *outboxWorld) expiredEvents() int {
	w.t.Helper()
	var n int
	if err := w.st.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE type = 'lease_expired'`).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func TestOtherWritesEnqueueNothing(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		ctx := context.Background()
		w := newOutboxWorld(t, dbs, nil)
		w.st.SetNotify(allKinds())
		held := w.lease(outboxAgent, "aa-1", 600)
		w.tick(time.Second)
		ttl := int64(900)
		if _, err := w.leases.Renew(ctx, outboxAgent, "key:x", held.Lease.Ref, leases.Renew{TTL: &ttl}); err != nil {
			t.Fatal(err)
		}
		w.tick(time.Second)
		if _, err := w.leases.End(ctx, outboxAgent, "key:x", held.Lease.Ref, "paused"); err != nil {
			t.Fatal(err)
		}
		w.wantRows()
		answered := w.ask(asks.New{Task: "aa-1", Question: "q1"})
		withdrawn := w.ask(asks.New{Task: "aa-1", Question: "q2"})
		w.wantRows("asking of ask_raised", "asking of ask_raised")
		before := len(w.rows())
		w.tick(time.Second)
		if _, err := w.asks.Answer(ctx, outboxOther, "key:y", answered.Ref, asks.Reply{Answer: "yes", Channel: "api"}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.asks.Withdraw(ctx, outboxAgent, "key:x", withdrawn.Ref, "no longer"); err != nil {
			t.Fatal(err)
		}
		if _, err := w.notes.Post(ctx, outboxAgent, "key:x", notes.New{Task: "aa-1", Summary: "a note"}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.tasks.Close(ctx, outboxAgent, "key:x", "aa-3", "dropped"); err != nil {
			t.Fatal(err)
		}
		if _, err := w.tasks.CloseInitiative(ctx, outboxAgent, "key:x", "aa", true); err != nil {
			t.Fatal(err)
		}
		if got := len(w.rows()); got != before {
			t.Fatalf("renew, end, answer, withdraw, note and close added %d rows", got-before)
		}
	})
}

func TestNotifyOnNarrowsTheKinds(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		ctx := context.Background()
		kinds, err := store.ParseNotifyOn("done")
		if err != nil {
			t.Fatal(err)
		}
		w := newOutboxWorld(t, dbs, kinds)
		w.lease(outboxAgent, "aa-1", 60)
		w.tick(2 * time.Minute)
		if _, err := w.leases.ExpireDue(ctx); err != nil {
			t.Fatal(err)
		}
		w.ask(asks.New{Task: "aa-1", Question: "q"})
		w.tick(time.Second)
		if _, err := w.tasks.Done(ctx, outboxAgent, "key:x", "aa-2", tasks.DoneOptions{}); err != nil {
			t.Fatal(err)
		}
		w.wantRows("done of task_done")
	})
}

func TestNoURLWritesNoRows(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		ctx := context.Background()
		w := newOutboxWorld(t, dbs, nil)
		w.lease(outboxAgent, "aa-1", 60)
		w.tick(2 * time.Minute)
		if _, err := w.leases.ExpireDue(ctx); err != nil {
			t.Fatal(err)
		}
		w.ask(asks.New{Question: "q"})
		if _, err := w.tasks.Done(ctx, outboxAgent, "key:x", "aa-2", tasks.DoneOptions{}); err != nil {
			t.Fatal(err)
		}
		w.wantRows()
	})
}

func TestARefusedWriteEnqueuesNothing(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		ctx := context.Background()
		w := newOutboxWorld(t, dbs, allKinds())
		w.tick(time.Second)
		if _, err := w.tasks.Done(ctx, outboxAgent, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
			t.Fatal(err)
		}
		w.wantRows("done of task_done")
		if _, err := w.tasks.Done(ctx, outboxAgent, "key:x", "aa-1", tasks.DoneOptions{}); err == nil {
			t.Fatal("marking a done task done succeeded")
		}
		if _, err := w.asks.Raise(ctx, outboxAgent, "key:x", asks.New{Task: "aa-99", Question: "q"}); err == nil {
			t.Fatal("an ask on an unknown task succeeded")
		}
		if _, err := w.asks.Raise(ctx, outboxAgent, "key:x", asks.New{Question: ""}); err == nil {
			t.Fatal("an ask with no question succeeded")
		}
		w.wantRows("done of task_done")
	})
}

func TestTheOutboxIsUniqueOnEventAndKind(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newOutboxWorld(t, dbs, allKinds())
		w.tick(time.Second)
		if _, err := w.tasks.Done(context.Background(), outboxAgent, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
			t.Fatal(err)
		}
		d := w.st.Dialect()
		insert := func(kind string) error {
			_, err := w.st.Writer().ExecContext(context.Background(), d.Rebind(`INSERT INTO notifications (id, kind, event_id, payload, state, attempts, next_attempt_at, created_at)
				SELECT ?, ?, event_id, '{}', 'pending', 0, 0, 0 FROM notifications LIMIT 1`), "X"+kind, kind)
			return err
		}
		if err := insert("done"); err == nil || !d.IsUniqueViolation(err) {
			t.Fatalf("a second done row for one event: %v, want a unique violation", err)
		}
		if err := insert("asking"); err != nil {
			t.Fatalf("a row of another kind for the same event: %v", err)
		}
	})
}

func TestTheNotificationsTableHasTheColumnsOfThePRD(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newOutboxWorld(t, dbs, allKinds())
		var rs *sql.Rows
		var err error
		rs, err = w.st.Reader().QueryContext(context.Background(), `SELECT id, kind, event_id, payload, state, attempts, next_attempt_at,
			claimed_until, last_error, created_at, sent_at FROM notifications`)
		if err != nil {
			t.Fatal(err)
		}
		_ = rs.Close()
	})
}

// TestEachTriggerEnqueuesTheDocumentedPayload reads the payload of the five cases (stranded, done, and asking on a
// task, an initiative and the board) on both databases.
func TestEachTriggerEnqueuesTheDocumentedPayload(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		ctx := context.Background()
		w := newOutboxWorld(t, dbs, allKinds())
		w.st.SetPublicURL("https://board.example/")
		holder := audit.Actor{Handle: "claude@host", WayBack: "tmux attach -t work"}
		ttl := int64(60)
		w.tick(time.Second)
		if _, err := w.leases.Start(ctx, holder, "key:x", leases.Start{Task: "aa-1", Intent: "write the parser", TTL: &ttl}); err != nil {
			t.Fatal(err)
		}
		w.tick(2 * time.Minute)
		if n, err := w.leases.ExpireDue(ctx); err != nil || n != 1 {
			t.Fatalf("ExpireDue = %d, %v", n, err)
		}
		for _, in := range []asks.New{{Task: "aa-2", Question: "which parser?", Blocking: true}, {Initiative: "aa", Question: "which scope?"}, {Question: "which board?"}} {
			w.tick(time.Second)
			if _, err := w.asks.Raise(ctx, holder, "key:x", in); err != nil {
				t.Fatal(err)
			}
		}
		w.tick(time.Second)
		if _, err := w.tasks.Done(ctx, holder, "key:x", "aa-3", tasks.DoneOptions{}); err != nil {
			t.Fatal(err)
		}
		want := []string{
			`{"text":"stranded: aa-1 t lease L1 expired (claude@host)\nintent: write the parser; way back: tmux attach -t work","kind":"stranded","task":{"ref":"aa-1","title":"t","url":"https://board.example/#/t/aa-1"},"actor":{"handle":"claude@host","way_back":"tmux attach -t work"},"lease":{"ref":"L1","intent":"write the parser","expired_at":"2026-11-05T12:01:04Z"},"board_url":"https://board.example/"}`,
			`{"text":"asking on aa-2 t: which parser? (claude@host, blocking)","kind":"asking","task":{"ref":"aa-2","title":"t","url":"https://board.example/#/t/aa-2"},"actor":{"handle":"claude@host","way_back":"tmux attach -t work"},"ask":{"ref":"A1","question":"which parser?","blocking":true},"board_url":"https://board.example/"}`,
			`{"text":"asking on initiative aa: which scope? (claude@host)","kind":"asking","initiative":{"slug":"aa"},"actor":{"handle":"claude@host","way_back":"tmux attach -t work"},"ask":{"ref":"A2","question":"which scope?","blocking":false},"board_url":"https://board.example/"}`,
			`{"text":"asking on board: which board? (claude@host)","kind":"asking","actor":{"handle":"claude@host","way_back":"tmux attach -t work"},"ask":{"ref":"A3","question":"which board?","blocking":false},"board_url":"https://board.example/"}`,
			`{"text":"done: aa-3 t (claude@host)","kind":"done","task":{"ref":"aa-3","title":"t","url":"https://board.example/#/t/aa-3"},"actor":{"handle":"claude@host","way_back":"tmux attach -t work"},"board_url":"https://board.example/"}`,
		}
		rs, err := w.st.Reader().QueryContext(ctx, `SELECT n.payload FROM notifications n JOIN events e ON e.id = n.event_id ORDER BY e.n`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rs.Close() }()
		var got []string
		for rs.Next() {
			var p string
			if err := rs.Scan(&p); err != nil {
				t.Fatal(err)
			}
			got = append(got, p)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("payloads =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})
}
