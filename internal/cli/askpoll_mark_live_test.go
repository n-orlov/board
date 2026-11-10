package cli

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// markWrite is the hold on the write of an end's commit mark (end_commit_ns):
// reached is closed when a write waits, release lets it go.
type markWrite struct {
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

// markWrites maps a database file to its hold.
var (
	markWrites   sync.Map // database file -> *markWrite
	markFuncOnce sync.Once
	markFuncErr  error
)

// registerMarkHold registers the scalar function the hold's trigger calls. A
// driver function reaches only connections opened later, so it runs before the
// board's store opens.
func registerMarkHold(t *testing.T) {
	t.Helper()
	markFuncOnce.Do(func() {
		markFuncErr = sqlite.RegisterScalarFunction("hold_end_commit_mark", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			path, _ := args[0].(string)
			if w, ok := markWrites.Load(path); ok {
				w := w.(*markWrite)
				w.once.Do(func() { close(w.reached) })
				<-w.release
			}
			return int64(0), nil
		})
	})
	if markFuncErr != nil {
		t.Fatal(markFuncErr)
	}
}

// holdMarkWrites makes the write of the end's commit mark wait until the
// returned release.
func (b *liveAskBoard) holdMarkWrites() *markWrite {
	w := &markWrite{reached: make(chan struct{}), release: make(chan struct{})}
	markWrites.Store(b.path, w)
	b.t.Cleanup(func() { markWrites.Delete(b.path) })
	trigger := fmt.Sprintf(`CREATE TRIGGER hold_mark BEFORE UPDATE OF end_commit_ns ON asks
		BEGIN SELECT hold_end_commit_mark('%s'); END`, strings.ReplaceAll(b.path, "'", "''"))
	if _, err := b.st.Writer().ExecContext(context.Background(), trigger); err != nil {
		b.t.Fatal(err)
	}
	return w
}

// An answer or withdrawal committed before the deadline, whose real-clock
// commit mark was sampled before it too, is exit 0 or 6 even when the write of
// that mark is held until after the deadline: the server's read at the deadline
// finds the end without a mark, and the wait takes the mark from a read made
// after it marked the poll behind that write. An end whose COMMIT returns after
// the deadline is exit 3 however its mark is written. The change feed polls
// once an hour, so the server learns of the end only by reading.
func TestAskPollLiveCountsAnEndWhoseTimelyMarkIsPersistedAfterTheDeadline(t *testing.T) {
	for _, c := range []struct {
		offset time.Duration // the end's last statement, from the deadline
		counts bool
	}{{-400 * time.Millisecond, true}, {-150 * time.Millisecond, true}, {100 * time.Millisecond, false}} {
		for _, e := range endCases {
			t.Run(fmt.Sprintf("withdraw=%v offset=%s", e.withdraw, c.offset), func(t *testing.T) {
				registerMarkHold(t)
				b := newLiveAskBoard(t)
				hold := b.holdMarkWrites()
				var once sync.Once
				ended := make(chan error, 1)
				b.mu.Lock()
				b.onGet = func(r *http.Request) {
					once.Do(func() {
						deadline, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("until"))
						if err != nil {
							t.Errorf("until: %v", err)
							return
						}
						go func() {
							time.Sleep(time.Until(deadline.Add(c.offset)))
							ended <- b.end(time.Now(), 0, e.withdraw)
						}()
						go func() {
							<-hold.reached
							time.Sleep(time.Until(deadline.Add(300 * time.Millisecond)))
							close(hold.release)
						}()
					})
				}
				b.mu.Unlock()
				code, out, _ := b.runLivePoll()
				if err := <-ended; err != nil {
					t.Fatal(err)
				}
				wantCode, wantOut := e.code, e.out
				if !c.counts {
					wantCode, wantOut = ExitTimeout, stillOpen
				}
				if code != wantCode || out != wantOut {
					t.Errorf("exit %d %q, want %d %q", code, out, wantCode, wantOut)
				}
			})
		}
	}
}
