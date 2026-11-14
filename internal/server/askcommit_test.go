package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
)

// commitHold holds the COMMIT of the next transaction that moves an ask out of
// open, after that transaction's last statement has run, until it is released.
// On SQLite it is the driver's commit hook of the database's writer connection;
// on Postgres a deferred constraint trigger, which runs at COMMIT, waits for an
// advisory lock the test holds. Either way the COMMIT itself is what waits.
type commitHold struct {
	arm     func()
	reached func() bool
	release func()
}

// sqliteHolds maps a database file to the hold of its writer's commits.
var (
	sqliteHolds    sync.Map // path -> *sqliteHold
	sqliteHookOnce sync.Once
)

type sqliteHold struct {
	mu      sync.Mutex
	armed   bool
	reached chan struct{}
	release chan struct{}
}

func (h *sqliteHold) commit() int32 {
	h.mu.Lock()
	armed := h.armed
	h.armed = false
	h.mu.Unlock()
	if armed {
		close(h.reached)
		<-h.release
	}
	return 0
}

func registerSQLiteHold(path string) *sqliteHold {
	h := &sqliteHold{reached: make(chan struct{}), release: make(chan struct{})}
	sqliteHolds.Store(path, h)
	installSQLiteHoldHook()
	return h
}

// installSQLiteHoldHook makes every SQLite connection opened from now on
// consult sqliteHolds. TestMain runs it before any test opens a database: the
// driver reads its hooks unlocked as it opens a connection, so registering one
// while a parallel test opens its store would be a data race.
func installSQLiteHoldHook() {
	sqliteHookOnce.Do(func() {
		sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
			if reg, ok := conn.(sqlite.HookRegisterer); ok {
				sqliteHolds.Range(func(path, h any) bool {
					if strings.Contains(dsn, path.(string)) {
						reg.RegisterCommitHook(h.(*sqliteHold).commit)
					}
					return true
				})
			}
			return nil
		})
	})
}

const pgHoldKey = 424242

// newCommitHold prepares a hold on the database at rawURL, to be called before
// the store opens it; install finishes it once the store has migrated.
func newCommitHold(t *testing.T, leg, rawURL string) (hold *commitHold, install func(*testing.T)) {
	t.Helper()
	if leg != "postgres" {
		h := registerSQLiteHold(strings.TrimPrefix(rawURL, "sqlite://"))
		return &commitHold{
			arm:     func() { h.mu.Lock(); h.armed = true; h.mu.Unlock() },
			reached: func() bool { return waitClosed(h.reached) },
			release: func() { close(h.release) },
		}, func(*testing.T) {}
	}
	db, err := sql.Open("pgx", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	var lock *sql.Conn
	return &commitHold{
			arm: func() {
				var err error
				if lock, err = db.Conn(ctx); err == nil {
					_, err = lock.ExecContext(ctx, "SELECT pg_advisory_lock($1)", pgHoldKey)
				}
				if err != nil {
					t.Error(err)
				}
			},
			reached: func() bool {
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					var n int
					_ = db.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted").Scan(&n)
					if n > 0 {
						return true
					}
					time.Sleep(5 * time.Millisecond)
				}
				return false
			},
			release: func() {
				_, _ = lock.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", pgHoldKey)
				_ = lock.Close()
			},
		}, func(t *testing.T) {
			t.Helper()
			for _, q := range []string{
				`CREATE FUNCTION hold_end_commit() RETURNS trigger LANGUAGE plpgsql AS
				 $$ BEGIN PERFORM pg_advisory_xact_lock(424242); RETURN NULL; END $$`,
				`CREATE CONSTRAINT TRIGGER hold_end_commit AFTER UPDATE ON asks DEFERRABLE INITIALLY DEFERRED
				 FOR EACH ROW WHEN (OLD.state = 'open' AND NEW.state <> 'open') EXECUTE FUNCTION hold_end_commit()`,
			} {
				if _, err := db.ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
		}
}

func waitClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// getUntil reads the ask with wait and until and returns the response document.
func (r *rig) getUntil(ref string, until time.Time) (int, map[string]any) {
	r.t.Helper()
	resp := r.do(http.MethodGet, "/api/v1/asks/"+ref+"?wait="+untilQuery("5", until), "", r.token)
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	return resp.StatusCode, doc
}

// end answers or withdraws ref through s.
func endAsk(s *asks.Store, withdraw bool, ref string) error {
	var err error
	if withdraw {
		_, err = s.Withdraw(context.Background(), audit.Actor{Handle: "claude@host"}, "key:x", ref, "moved on")
	} else {
		_, err = s.Answer(context.Background(), audit.Actor{Handle: "nik@host", Operator: true}, "key:x", ref, asks.Reply{Answer: "late"})
	}
	return err
}

func endFields(doc map[string]any) bool {
	for _, f := range []string{"answered_at", "withdrawn_at", "answer", "ended_at", "withdraw_reason"} {
		if _, ok := doc[f]; ok {
			return true
		}
	}
	return false
}

// An answer or withdrawal whose last statement runs before until but whose COMMIT
// is held until after it never counts: the read using that until, which starts
// only after the COMMIT returned, finds the ask committed and answers it open,
// with none of the end's fields. Neither an in-transaction stamp nor a clock
// read before the COMMIT is proof. Both databases.
func TestAnEndWhoseCommitReturnsAfterUntilIsNeverAnsweredEnded(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, leg string, dbs databases) {
		for _, withdraw := range []bool{false, true} {
			url := dbs.New(t)
			hold, install := newCommitHold(t, leg, url)
			st := openStore(t, url)
			if err := st.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			install(t)
			r, as := askRig(t, st, newToken(t, st), newTestClock(), time.Hour)
			ref := r.raiseAsk()
			until := time.Now().Add(500 * time.Millisecond)
			ender := &asks.Store{Store: st}
			hold.arm()
			ended := make(chan error, 1)
			go func() { ended <- endAsk(ender, withdraw, ref) }()
			if !hold.reached() {
				t.Fatalf("withdraw=%v: the end never reached its COMMIT", withdraw)
			}
			if !time.Now().Before(until) {
				t.Fatalf("withdraw=%v: the end reached its COMMIT after until: the test is too slow to mean anything", withdraw)
			}
			time.Sleep(time.Until(until.Add(150 * time.Millisecond)))
			if k, err := as.Get(context.Background(), ref); err != nil || k.State != "open" {
				t.Fatalf("withdraw=%v: the ask reads %q (%v) while its COMMIT is held, want open", withdraw, k.State, err)
			}
			hold.release()
			if err := <-ended; err != nil {
				t.Fatal(err)
			}
			if k, err := as.Get(context.Background(), ref); err != nil || k.State == "open" {
				t.Fatalf("withdraw=%v: the end did not commit (%+v, %v)", withdraw, k, err)
			}
			status, doc := r.getUntil(ref, until) // the first read using this until, after the COMMIT
			if status != http.StatusOK || doc["state"] != "open" || endFields(doc) {
				t.Errorf("withdraw=%v: got %d %v, want 200 and the ask open with none of the end's fields", withdraw, status, doc)
			}
		}
	})
}
