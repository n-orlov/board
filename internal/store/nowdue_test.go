package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/store"
)

// dueSeed is a board every Now read below meets with due leases: tasks tasks in
// initiative aa, each with one live lease that falls due; supersede puts a later
// lease on each of those tasks ("ended" an ended one, "live" a live one not due
// for days), which leaves the due lease not the newest on its task, so it raises
// no stranded alert; olderEnded puts an ended lease before it; liveTasks tasks in
// initiative bb hold a live lease not due, so they stay in flight.
type dueSeed struct {
	tasks      int
	supersede  string
	olderEnded bool
	liveTasks  int
	// endedTasks tasks in initiative bb hold one ended lease and nothing else
	endedTasks int
}

// seedDue writes s onto a board that holds only the initiatives aa and bb (both
// tagged web), straight through SQL, with a holder of its own, and returns how
// many due leases are the newest on their task.
func seedDue(t *testing.T, st *store.Store, s dueSeed) (newest int) {
	t.Helper()
	ctx := context.Background()
	tx, err := st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	const hour = int64(time.Hour / time.Millisecond)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC).UnixMilli()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, st.Dialect().Rebind(q), args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO actors (id, handle, kind, way_back, first_seen_at, last_seen_at) VALUES ('seed-agent', 'seeder@host', 'agent', 'tmux 9', ?, ?)`,
		base-5*hour, base-5*hour)
	iniOf := func(slug string) string {
		var id string
		if err := tx.QueryRowContext(ctx, st.Dialect().Rebind(`SELECT id FROM initiatives WHERE slug = ?`), slug).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	task := func(id, ini string, seq int) {
		exec(`INSERT INTO tasks (id, initiative_id, seq, title, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, 'seed-agent', ?, ?)`,
			id, ini, seq, "seeded "+id, base-4*hour, base-4*hour)
	}
	lease := func(id, task, state string, started, expires int64) {
		var ended any
		if state == "ended" {
			ended = expires
		}
		exec(`INSERT INTO leases (id, task_id, actor_id, intent, ttl_seconds, started_at, renewed_at, expires_at, state, ended_at)
			VALUES (?, ?, 'seed-agent', 'bulk', 3600, ?, ?, ?, ?, ?)`,
			id, task, started, started, expires, state, ended)
	}
	aa, bb := iniOf("aa"), iniOf("bb")
	for i := 1; i <= s.tasks; i++ {
		id := fmt.Sprintf("due-%d", i)
		task(id, aa, i)
		if s.olderEnded {
			lease(id+"-old", id, "ended", base-3*hour, base-2*hour)
		}
		lease(id+"-due", id, "live", base-2*hour, base+hour)
		switch s.supersede {
		case "ended":
			lease(id+"-new", id, "ended", base-hour, base)
		case "live":
			lease(id+"-new", id, "live", base-hour, base+100*hour)
		default:
			newest++
		}
	}
	for i := 1; i <= s.liveTasks; i++ {
		id := fmt.Sprintf("live-%d", i)
		task(id, bb, i)
		lease(id+"-live", id, "live", base-hour, base+100*hour)
	}
	for i := 1; i <= s.endedTasks; i++ {
		id := fmt.Sprintf("ended-%d", i)
		task(id, bb, 1000+i)
		lease(id+"-end", id, "ended", base-3*hour, base-2*hour)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return newest
}

// dueOutcome is what one Now read of a seeded board did.
type dueOutcome struct {
	statements                                  int64
	newest, inFlight, named                     int
	expired, expiredEvents, alerts, live, ended int
}

// readDueBoard seeds s on an otherwise empty board, moves the clock 48 hours on,
// so every lease due an hour after the seed's start has run out, and runs one
// Now read narrowed by q.
func readDueBoard(t *testing.T, s dueSeed, q attention.Query, alerts ...string) dueOutcome {
	t.Helper()
	return readDueBoardOn(context.Background(), t, "sqlite", s, q, alerts...)
}

// readDueBoardOn is readDueBoard on the database of the leg.
// The read runs under readCtx (a request that names an operator's origin
// writes through one more statement than a read that names none).
func readDueBoardOn(readCtx context.Context, t *testing.T, leg string, s dueSeed, q attention.Query, alerts ...string) dueOutcome {
	t.Helper()
	ctx := context.Background()
	a, count, advance := boardOn(t, leg, 0, alerts...)
	var o dueOutcome
	o.newest = seedDue(t, a.Store, s)
	advance(48 * time.Hour)
	before := count.Load()
	b, err := a.Board(readCtx, q, attention.Window24h)
	if err != nil {
		t.Fatal(err)
	}
	o.statements = count.Load() - before
	o.inFlight, o.named = len(b.InFlight), len(b.TaskMarks)
	if err := a.Store.Reader().QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM leases WHERE state = 'expired'), (SELECT COUNT(*) FROM events WHERE type = 'lease_expired'),
		(SELECT COUNT(*) FROM notifications WHERE kind = 'stranded'), (SELECT COUNT(*) FROM leases WHERE state = 'live'),
		(SELECT COUNT(*) FROM leases WHERE state = 'ended')`).
		Scan(&o.expired, &o.expiredEvents, &o.alerts, &o.live, &o.ended); err != nil {
		t.Fatal(err)
	}
	return o
}

// checkDueOutcome fails when the read did not expire exactly the due leases of s,
// each with its event, raise exactly the alerts of the newest ones (when on), and
// leave every other lease as it was.
func checkDueOutcome(t *testing.T, name string, s dueSeed, o dueOutcome, alertsOn bool) {
	t.Helper()
	wantAlerts, wantLive, wantEnded := 0, s.liveTasks, s.endedTasks
	if alertsOn {
		wantAlerts = o.newest
	}
	if s.supersede == "live" {
		wantLive += s.tasks
	}
	if s.supersede == "ended" {
		wantEnded += s.tasks
	}
	if s.olderEnded {
		wantEnded += s.tasks
	}
	if o.expired != s.tasks || o.expiredEvents != s.tasks || o.alerts != wantAlerts || o.live != wantLive || o.ended != wantEnded {
		t.Errorf("%s: seeded %+v; the read left %d expired with %d events and %d alerts (want %d), %d live (want %d), %d ended (want %d)",
			name, s, o.expired, o.expiredEvents, o.alerts, wantAlerts, o.live, wantLive, o.ended, wantEnded)
	}
}

func TestTheNowReadRunsTheSameStatementsWhateverMixOfDueLiveAndEndedLeasesItMeets(t *testing.T) {
	boards := []struct {
		name string
		seed dueSeed
	}{
		{"every due lease raises an alert, live and ended leases beside", dueSeed{tasks: 35, olderEnded: true, liveTasks: 5}},
		// the case review found: a newer ended lease on every due lease's
		// task leaves the stranded alerts with no row to write
		{"no due lease raises an alert: a newer ended lease on each task", dueSeed{tasks: 27, supersede: "ended", olderEnded: true, liveTasks: 5}},
		{"no due lease raises an alert: a newer live lease on each task", dueSeed{tasks: 27, supersede: "live", olderEnded: true}},
		{"nothing is left in flight once the due leases expire", dueSeed{tasks: 30, olderEnded: true}},
		{"no ended lease anywhere", dueSeed{tasks: 30, liveTasks: 5}},
		{"a single due lease and nothing else", dueSeed{tasks: 1}},
		{"past the old chunks of 1000 rows and 500 leases in flight", dueSeed{tasks: 1200, olderEnded: true, liveTasks: 1100}},
	}
	for name, alerts := range map[string][]string{"alerts off": nil, "stranded alerts on": {store.NotifyStranded}} {
		var first dueOutcome
		for i, b := range boards {
			o := readDueBoard(t, b.seed, attention.Query{}, alerts...)
			checkDueOutcome(t, name+", "+b.name, b.seed, o, alerts != nil)
			if i == 0 {
				first = o
				if o.inFlight == 0 || o.statements == 0 {
					t.Fatalf("%s: the first board has %d tasks in flight and ran %d statements, so it proves nothing", name, o.inFlight, o.statements)
				}
				continue
			}
			if o.statements != first.statements {
				t.Errorf("%s: %d statements on %q (%d due, %d alerts, %d in flight) but %d on %q (%d due, %d alerts, %d in flight): the read is not constant",
					name, first.statements, boards[0].name, boards[0].seed.tasks, first.alerts, first.inFlight,
					o.statements, b.name, b.seed.tasks, o.alerts, o.inFlight)
			}
		}
	}
}

func TestTheNowReadRunsTheSameStatementsWhenItsFilterNamesNoTaskOnABoardWithDueLeases(t *testing.T) {
	seed := dueSeed{tasks: 30, olderEnded: true, liveTasks: 10}
	// both initiatives are tagged web, so the first filter names every task
	// and the second, a tag nothing has, names none
	all := readDueBoard(t, seed, attention.Query{Tags: []string{"web"}}, store.NotifyStranded)
	none := readDueBoard(t, seed, attention.Query{Tags: []string{"nothing"}}, store.NotifyStranded)
	if all.named == 0 || none.named != 0 {
		t.Fatalf("the web filter named %d tasks and the other %d, so it proves nothing", all.named, none.named)
	}
	if all.statements != none.statements {
		t.Errorf("%d statements when the filter names %d tasks but %d when it names none: the read is not constant",
			all.statements, all.named, none.statements)
	}
	// the expiry is not narrowed by the filter: both reads expired every due lease
	checkDueOutcome(t, "web", seed, all, true)
	checkDueOutcome(t, "nothing", seed, none, true)
}

// steadyBoards are boards that meet zero, one and many due leases, mixed with
// live and ended ones, some of the due leases raising no stranded alert.
var steadyBoards = []struct {
	name string
	seed dueSeed
}{
	{"nothing at all", dueSeed{}},
	{"live and ended leases and none due", dueSeed{liveTasks: 5, endedTasks: 5}},
	{"one due lease that raises an alert", dueSeed{tasks: 1, liveTasks: 5, endedTasks: 5}},
	{"one due lease that raises no alert", dueSeed{tasks: 1, supersede: "ended", liveTasks: 5, endedTasks: 5}},
	{"many due leases that all raise an alert", dueSeed{tasks: 40, olderEnded: true, liveTasks: 5, endedTasks: 5}},
	{"many due leases that raise none: a newer live lease on each", dueSeed{tasks: 40, supersede: "live", olderEnded: true, liveTasks: 5, endedTasks: 5}},
	{"many due leases that raise none: a newer ended lease on each", dueSeed{tasks: 40, supersede: "ended", liveTasks: 5, endedTasks: 5}},
}

func TestTheNowReadRunsTheSameStatementsWithZeroOneAndManyDueLeasesOnEveryLeg(t *testing.T) {
	requests := map[string]context.Context{
		"no request origin":       context.Background(),
		"an operator origin":      audit.WithOrigin(context.Background(), "nik@host"),
		"an origin naming nobody": audit.WithOrigin(context.Background(), ""),
	}
	eachLeg(t, func(t *testing.T, leg string) {
		for alertName, alerts := range map[string][]string{"alerts off": nil, "stranded alerts on": {store.NotifyStranded}} {
			for reqName, ctx := range requests {
				want := int64(-1)
				for _, b := range steadyBoards {
					o := readDueBoardOn(ctx, t, leg, b.seed, attention.Query{}, alerts...)
					checkDueOutcome(t, alertName+", "+b.name, b.seed, o, alerts != nil)
					if o.statements == 0 {
						t.Fatalf("%s, %s, %s: the read counted no statements, so the counter is not wired", alertName, reqName, b.name)
					}
					if want < 0 {
						want = o.statements
					}
					if o.statements != want {
						t.Errorf("%s, %s: %d statements on %q but %d on %q (%d due, %d expired, %d alerts): the read is not constant",
							alertName, reqName, o.statements, b.name, want, steadyBoards[0].name, b.seed.tasks, o.expired, o.alerts)
					}
				}
			}
		}
	})
}

func TestANoDueNowReadCompletesWhileTheSQLiteWriterIsHeld(t *testing.T) {
	for alertName, alerts := range map[string][]string{"alerts off": nil, "stranded alerts on": {store.NotifyStranded}} {
		a, _, advance := boardOfSize(t, 0, alerts...)
		seedDue(t, a.Store, dueSeed{liveTasks: 5, endedTasks: 5})
		advance(time.Hour)
		tx, err := a.Store.Writer().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		b, err := a.Board(ctx, attention.Query{}, attention.Window24h)
		cancel()
		_ = tx.Rollback()
		if err != nil {
			t.Fatalf("%s: a read with nothing due while the writer connection is held: %v", alertName, err)
		}
		if len(b.InFlight) != 5 {
			t.Errorf("%s: %d tasks in flight, want the 5 live ones", alertName, len(b.InFlight))
		}
	}
}

func TestIdleRunsExactlyTheStatementsAskedForOnTheReaderWithoutTheWriter(t *testing.T) {
	var count atomic.Int64
	st, err := store.OpenCounting(filepath.Join(t.TempDir(), "b.db"), &count)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	held, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, n := range []int{0, 1, 4, 7} {
		before := count.Load()
		if err := st.Idle(ctx, n); err != nil {
			t.Fatalf("Idle(%d) with the writer held: %v", n, err)
		}
		if got := count.Load() - before; got != int64(n) {
			t.Errorf("Idle(%d) ran %d statements", n, got)
		}
	}
}
