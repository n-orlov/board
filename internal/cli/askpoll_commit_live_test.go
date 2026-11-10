package cli

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// commitHolds maps a database file to the hold on the COMMIT of its writer.
// The driver's connection hook is global and cannot be removed, so it is
// registered once; each connection's commit hook looks the hold up by the
// connection's database file when a COMMIT runs, so a test registers its hold
// after its store is open.
var (
	commitHolds    sync.Map // database file -> *commitHold
	commitHookOnce sync.Once
)

// commitHold holds the COMMIT of the next transaction that runs while it is
// armed, after that transaction's last statement, until it is released.
type commitHold struct {
	mu      sync.Mutex
	armed   bool
	reached chan struct{}
	release chan struct{}
}

func (h *commitHold) commit() int32 {
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

// installCommitHook makes every SQLite connection opened from now on consult
// the commit holds. It runs before the store of the board under test is open.
func installCommitHook() {
	commitHookOnce.Do(func() {
		sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
			if reg, ok := conn.(sqlite.HookRegisterer); ok {
				reg.RegisterCommitHook(func() int32 {
					var found *commitHold
					commitHolds.Range(func(path, h any) bool {
						if strings.Contains(dsn, path.(string)) {
							found = h.(*commitHold)
						}
						return found == nil
					})
					if found == nil {
						return 0
					}
					return found.commit()
				})
			}
			return nil
		})
	})
}

// newCommitHeldBoard is a live board whose writer has a commit hold.
func newCommitHeldBoard(t *testing.T) (*liveAskBoard, *commitHold) {
	installCommitHook()
	b := newLiveAskBoard(t)
	h := &commitHold{reached: make(chan struct{}), release: make(chan struct{})}
	commitHolds.Store(b.path, h)
	t.Cleanup(func() { commitHolds.Delete(b.path) })
	return b, h
}

// pollCommitHeldEnd runs one poll in which the end's last statement runs at
// endAt, well before the deadline, but its COMMIT is held until release, after
// the deadline. prep sets up whatever else the case delays. It checks the end
// really reached its COMMIT before the deadline, that the ask was still open at
// the deadline, and that the COMMIT returned after it, and returns the poll's exit code, output and
// duration.
func pollCommitHeldEnd(t *testing.T, withdraw bool, endAt, release time.Duration, prep func(b *liveAskBoard, start time.Time)) (int, string, time.Duration) {
	b, hold := newCommitHeldBoard(t)
	watch := b.watcher()
	start := time.Now()
	prep(b, start)
	reached := make(chan time.Time, 1)
	done := make(chan error, 1)
	go func() {
		time.Sleep(time.Until(start.Add(endAt)))
		hold.mu.Lock()
		hold.armed = true
		hold.mu.Unlock()
		go func() {
			<-hold.reached
			reached <- time.Now()
		}()
		done <- b.end(start, 0, withdraw)
	}()
	atDeadline := stateBy(watch, start, 1050*time.Millisecond)
	released := make(chan time.Time, 1)
	go func() {
		time.Sleep(time.Until(start.Add(release)))
		released <- time.Now()
		close(hold.release)
	}()
	code, out, took := b.runLivePoll()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	armedBy, releasedAt := <-reached, <-released
	if !armedBy.Before(start.Add(time.Second)) {
		t.Fatalf("withdraw=%v: the end reached its COMMIT at %s, want before the deadline", withdraw, armedBy.Sub(start))
	}
	if s := <-atDeadline; s != "open" {
		t.Fatalf("withdraw=%v: the ask was %q at the deadline, want still open", withdraw, s)
	}
	k, err := watch.Get(context.Background(), "A1")
	if err != nil || k.State == "open" || releasedAt.Before(start.Add(time.Second)) {
		t.Fatalf("withdraw=%v: the end is %q (%v) released at %s, want committed once released after the deadline", withdraw, k.State, err, releasedAt.Sub(start))
	}
	return code, out, took
}

// An answer or withdrawal whose last statement runs before the deadline but
// whose COMMIT is held until after it is exit 3 with the still-open line when
// the poll's first read reaches the server's handler only after that COMMIT:
// the ask is committed by then, but not by the deadline.
func TestAskPollLiveEndWhoseCommitReturnsAfterTheDeadlineIsOpenForAFirstReadAfterIt(t *testing.T) {
	for _, e := range endCases {
		code, out, took := pollCommitHeldEnd(t, e.withdraw, 300*time.Millisecond, 1300*time.Millisecond, func(b *liveAskBoard, _ time.Time) {
			b.early = 1600 * time.Millisecond
		})
		if code != ExitTimeout || out != stillOpen || took < 1600*time.Millisecond {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 once the queued read is answered", e.withdraw, code, out, took)
		}
	}
}

// The same end is exit 3 with the still-open line when the server's readers
// are all taken across the deadline, until after the COMMIT returned, so the
// read the server makes at the deadline runs only then and finds the end.
func TestAskPollLiveEndWhoseCommitReturnsAfterTheDeadlineIsOpenWhileTheReadersAreTaken(t *testing.T) {
	for _, e := range endCases {
		code, out, took := pollCommitHeldEnd(t, e.withdraw, 300*time.Millisecond, 1300*time.Millisecond, func(b *liveAskBoard, start time.Time) {
			b.early = 200 * time.Millisecond
			b.hogReaders(start, 900*time.Millisecond, 1450*time.Millisecond)
		})
		if code != ExitTimeout || out != stillOpen || took < 1400*time.Millisecond {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 once the readers are free", e.withdraw, code, out, took)
		}
	}
}

// An end whose COMMIT returns after the deadline is exit 3 with the still-open
// line even when the write of its real-clock commit mark is held as well: the
// mark is sampled after that COMMIT, so it is after the deadline, and taking a
// mark persisted late must not turn it into proof.
func TestAskPollLiveEndWhoseCommitReturnsAfterTheDeadlineIsOpenWhenItsMarkWriteIsHeldToo(t *testing.T) {
	for _, e := range endCases {
		for _, release := range []time.Duration{1100 * time.Millisecond, 1300 * time.Millisecond} {
			registerMarkHold(t)
			code, out, took := pollCommitHeldEnd(t, e.withdraw, 300*time.Millisecond, release, func(b *liveAskBoard, start time.Time) {
				b.early = 200 * time.Millisecond
				mark := b.holdMarkWrites()
				go func() {
					time.Sleep(time.Until(start.Add(1500 * time.Millisecond)))
					close(mark.release)
				}()
			})
			if code != ExitTimeout || out != stillOpen {
				t.Errorf("withdraw=%v release=%s: got %d %q after %s, want exit 3 with the still-open line", e.withdraw, release, code, out, took)
			}
		}
	}
}

// The same end is exit 3 when the COMMIT returns only just after the deadline
// and the readers are taken across it, however briefly after: no tolerance
// is given to a COMMIT that returned after the deadline.
func TestAskPollLiveEndWhoseCommitReturnsJustAfterTheDeadlineIsOpenWhileTheReadersAreTaken(t *testing.T) {
	for _, e := range endCases {
		code, out, took := pollCommitHeldEnd(t, e.withdraw, 700*time.Millisecond, 1100*time.Millisecond, func(b *liveAskBoard, start time.Time) {
			b.early = 200 * time.Millisecond
			b.hogReaders(start, 900*time.Millisecond, 1450*time.Millisecond)
		})
		if code != ExitTimeout || out != stillOpen {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 with the still-open line", e.withdraw, code, out, took)
		}
	}
}
