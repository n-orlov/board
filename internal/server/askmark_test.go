package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/store"
)

// markHold holds the write of an end's commit mark (end_commit_ns), which is
// made after the end's COMMIT returned and the real clock was read, until it is
// released: the mark is then sampled in time but persisted late. On SQLite a
// trigger calls a scalar function that waits on the hold; on Postgres it waits
// for an advisory lock the test takes.
type markHold struct {
	arm     func()
	reached func() bool
	release func()
}

const pgMarkKey = 424243

var (
	markHolds    sync.Map // database file -> chan struct{} closed to release
	markReached  sync.Map // database file -> chan struct{} closed when a write waits
	markFuncOnce sync.Once
	markFuncErr  error
)

// registerMarkFunc registers the scalar function the SQLite trigger calls. A
// driver function reaches only connections opened after it is registered, so
// this runs before the store opens.
func registerMarkFunc(t *testing.T) {
	t.Helper()
	markFuncOnce.Do(func() {
		markFuncErr = sqlite.RegisterScalarFunction("hold_end_commit_mark", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			p, _ := args[0].(string)
			release, ok := markHolds.Load(p)
			if !ok {
				return int64(0), nil
			}
			reached, _ := markReached.Load(p)
			select {
			case <-reached.(chan struct{}):
			default:
				close(reached.(chan struct{}))
			}
			<-release.(chan struct{})
			return int64(0), nil
		})
	})
	if markFuncErr != nil {
		t.Fatal(markFuncErr)
	}
}

func newMarkHold(t *testing.T, leg string, st *store.Store, rawURL string) *markHold {
	t.Helper()
	ctx := context.Background()
	if leg == "postgres" {
		return newPostgresMarkHold(t, st, rawURL)
	}
	path := strings.TrimPrefix(rawURL, "sqlite://")
	release, reached := make(chan struct{}), make(chan struct{})
	markHolds.Store(path, release)
	markReached.Store(path, reached)
	t.Cleanup(func() { markHolds.Delete(path); markReached.Delete(path) })
	trigger := fmt.Sprintf(`CREATE TRIGGER hold_mark BEFORE UPDATE OF end_commit_ns ON asks
		BEGIN SELECT hold_end_commit_mark('%s'); END`, strings.ReplaceAll(path, "'", "''"))
	if _, err := st.Writer().ExecContext(ctx, trigger); err != nil {
		t.Fatal(err)
	}
	return &markHold{arm: func() {}, reached: func() bool { return waitClosed(reached) }, release: func() { close(release) }}
}

func newPostgresMarkHold(t *testing.T, st *store.Store, rawURL string) *markHold {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		fmt.Sprintf(`CREATE FUNCTION hold_end_commit_mark() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN PERFORM pg_advisory_lock(%d); PERFORM pg_advisory_unlock(%d); RETURN NEW; END $$`, pgMarkKey, pgMarkKey),
		`CREATE TRIGGER hold_mark BEFORE UPDATE OF end_commit_ns ON asks FOR EACH ROW EXECUTE FUNCTION hold_end_commit_mark()`,
	} {
		if _, err := st.Writer().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("pgx", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var lock *sql.Conn
	return &markHold{
		arm: func() {
			var err error
			if lock, err = db.Conn(ctx); err == nil {
				_, err = lock.ExecContext(ctx, "SELECT pg_advisory_lock($1)", pgMarkKey)
			}
			if err != nil {
				t.Error(err)
			}
		},
		reached: func() bool {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				var n int
				_ = db.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND objid = $1", pgMarkKey).Scan(&n)
				if n > 0 {
					return true
				}
				time.Sleep(5 * time.Millisecond)
			}
			return false
		},
		release: func() {
			_, _ = lock.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", pgMarkKey)
			_ = lock.Close()
		},
	}
}

// markCase is one end whose commit mark is sampled at endAt from the wait's
// start (until is one second out) but written only after until.
type markCase struct {
	name     string
	withdraw bool
	endAt    time.Duration
	counts   bool
	readAt   time.Duration // when the wait's request reaches the server, from until; 0 is at the start
}

// An end whose COMMIT returned, and whose real-clock mark was sampled, at or
// before until counts even when the write of that mark is still held up when
// until passes (the read at until finds the end without a mark) and runs past
// it: the wait marks the poll behind that write and takes the mark from a read
// made after. An end whose COMMIT returned after until is never counted, and
// the mark persisted late by it is no proof. Both databases.
func TestAWaitCountsAnEndWhoseTimelyMarkIsPersistedAfterUntil(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		for _, c := range []markCase{
			{"answer, mark 400ms early", false, 600 * time.Millisecond, true, 0},
			{"withdrawal, mark 400ms early", true, 600 * time.Millisecond, true, 0},
			{"answer, mark 150ms early", false, 850 * time.Millisecond, true, 0},
			{"withdrawal, mark 150ms early", true, 850 * time.Millisecond, true, 0},
			{"answer committed 100ms late", false, 1100 * time.Millisecond, false, 0},
			{"withdrawal committed 100ms late", true, 1100 * time.Millisecond, false, 0},
		} {
			t.Run(c.name, func(t *testing.T) { testTimelyMarkPersistedLate(t, leg, dbs, c) })
		}
	})
}

// The same holds when the wait's request reaches the server only after until,
// finds the end already committed and its mark still being written: that read
// is no proof (it came back after until), so the wait takes the mark once the
// write lands, and counts the end only if the mark is at or before until.
func TestAWaitReachingTheServerAfterUntilCountsAnEndWhoseTimelyMarkIsPersistedLate(t *testing.T) {
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		for _, c := range []markCase{
			{"answer, mark 400ms early", false, 600 * time.Millisecond, true, 100 * time.Millisecond},
			{"withdrawal, mark 400ms early", true, 600 * time.Millisecond, true, 100 * time.Millisecond},
			{"answer, mark 150ms early", false, 850 * time.Millisecond, true, 100 * time.Millisecond},
			{"withdrawal committed 100ms late", true, 1100 * time.Millisecond, false, 150 * time.Millisecond},
			{"answer committed 100ms late", false, 1100 * time.Millisecond, false, 150 * time.Millisecond},
		} {
			t.Run(c.name, func(t *testing.T) { testTimelyMarkPersistedLate(t, leg, dbs, c) })
		}
	})
}

// timelyAttempts is how many times a case whose end is to be timely runs
// before it gives up: see testTimelyMarkPersistedLate.
const timelyAttempts = 5

// testTimelyMarkPersistedLate runs c. An end meant to be timely is started a
// fixed time before until on the real clock, but whether its COMMIT returns and
// its mark is sampled by until is up to the scheduler: on a loaded host the end
// can run late, and then it is not the case under test (it is a late end, which
// the other cases cover). The persisted mark says which it was, since the mark
// is the real clock read after the COMMIT returned: a mark after until means
// the end was late, and the attempt is run again on a fresh database rather
// than judged. Every attempt whose mark is at or before until is judged in
// full, so an end that is timely and not counted still fails.
func testTimelyMarkPersistedLate(t *testing.T, leg string, dbs databases, c markCase) {
	for attempt := 1; ; attempt++ {
		res, k, until, err := runTimelyMark(t, leg, dbs, c)
		late := err == nil && k.EndCommitAt != nil && k.EndCommitAt.After(until)
		if c.counts && late {
			if attempt < timelyAttempts {
				t.Logf("attempt %d: the end's mark %v came after until %v (the host ran it late), so it was not timely; again", attempt, *k.EndCommitAt, until)
				continue
			}
			t.Fatalf("in %d attempts the end's mark never came by until (last %v, until %v): the host is too loaded to run a timely end", attempt, *k.EndCommitAt, until)
		}
		want := "open"
		if c.counts {
			want = "answered"
			if c.withdraw {
				want = "withdrawn"
			}
		}
		if res.status != http.StatusOK || res.doc["state"] != want {
			t.Errorf("got %d %v, want 200 and state %q", res.status, res.doc, want)
		}
		if endFields(res.doc) != c.counts {
			t.Errorf("end fields present = %v in %v, want %v", endFields(res.doc), res.doc, c.counts)
		}
		if err != nil || k.EndCommitAt == nil || k.EndCommitAt.After(until) == c.counts {
			t.Errorf("persisted mark %v (%v), want one at or before until: %v", k.EndCommitAt, err, c.counts)
		}
		return
	}
}

// markResult is what a wait returned.
type markResult struct {
	status int
	doc    map[string]any
}

// runTimelyMark runs c once on a fresh database, and returns what the wait
// returned, the ask as persisted once the end's mark was written, and until.
func runTimelyMark(t *testing.T, leg string, dbs databases, c markCase) (markResult, asks.Ask, time.Time, error) {
	t.Helper()
	registerMarkFunc(t)
	url := dbs.New(t)
	st := openStore(t, url)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	hold := newMarkHold(t, leg, st, url)
	// the feed polls once an hour, so only the read at until sees the end
	as := &asks.Store{Store: st}
	r := startRig(t, st, newToken(t, st), streamSettings{poll: time.Hour, heartbeat: time.Minute, extra: func(*store.Store) []Option {
		return []Option{WithAsks(as), WithAskTouch(time.Hour)}
	}})
	ref := r.raiseAsk()
	hold.arm()
	until := time.Now().Add(time.Second)
	got := make(chan markResult, 1)
	go func() {
		if c.readAt != 0 {
			time.Sleep(time.Until(until.Add(c.readAt)))
		}
		status, doc := r.getUntil(ref, until)
		got <- markResult{status, doc}
	}()
	ended := make(chan error, 1)
	go func() {
		time.Sleep(time.Until(until.Add(c.endAt - time.Second)))
		ended <- endAsk(&asks.Store{Store: st}, c.withdraw, ref)
	}()
	if !hold.reached() {
		t.Fatal("the end's mark write never started")
	}
	time.Sleep(time.Until(until.Add(300 * time.Millisecond)))
	hold.release()
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	res := <-got
	k, err := as.Get(context.Background(), ref)
	return res, k, until, err
}
