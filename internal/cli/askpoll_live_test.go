package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/store"
)

// liveAskBoard serves the routes `board ask --poll` uses from a real SQLite
// asks.Store, in real time: the raise, a ?wait= read held until the ask leaves
// open or the client goes, and a plain read.
type liveAskBoard struct {
	t   *testing.T
	s   *asks.Store
	mu  sync.Mutex
	ref string
	// delay holds the response of a ?wait= read that finds the end this long
	delay time.Duration
	// plainDelay holds the response of every plain read this long
	plainDelay time.Duration
	raisedAt   time.Time
}

func newLiveAskBoard(t *testing.T) *liveAskBoard {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &liveAskBoard{t: t, s: &asks.Store{Store: st}}
}

func (b *liveAskBoard) write(w http.ResponseWriter, code int, k asks.Ask) {
	v := storedAskView(k)
	v.AnsweredBy, v.WithdrawReason = k.AnsweredBy, k.WithdrawReason
	body, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (b *liveAskBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		k, err := b.s.Raise(ctx, audit.Actor{Handle: "claude@host"}, "key:x", asks.New{Question: "Which port?", Mode: "poll"})
		if err != nil {
			b.t.Error(err)
		}
		b.mu.Lock()
		b.ref, b.raisedAt = k.Ref, time.Now()
		b.mu.Unlock()
		b.write(w, http.StatusCreated, k)
		return
	}
	if r.URL.Query().Get("wait") == "" && b.plainDelay > 0 {
		k, err := b.s.Get(ctx, b.askRef()) // read now, answered later
		select {
		case <-ctx.Done():
			return
		case <-time.After(b.plainDelay):
		}
		if err == nil {
			b.write(w, http.StatusOK, k)
		}
		return
	}
	for {
		k, err := b.s.Get(ctx, b.askRef())
		if err != nil || k.State != "open" || r.URL.Query().Get("wait") == "" {
			if err == nil && r.URL.Query().Get("wait") != "" && b.delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(b.delay):
				}
			}
			if err == nil {
				b.write(w, http.StatusOK, k)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// endAt answers or withdraws the ask at after from its raise, and reports
// whether it had committed before the poll's deadline (a second after the raise
// at the earliest, since the CLI starts its clock once the raise returns).
func (b *liveAskBoard) endAt(after time.Duration, withdraw bool) <-chan bool {
	done := make(chan bool, 1)
	go func() {
		for b.askRef() == "" {
			time.Sleep(time.Millisecond)
		}
		b.mu.Lock()
		raised := b.raisedAt
		b.mu.Unlock()
		time.Sleep(time.Until(raised.Add(after)))
		err := b.end(withdraw)
		done <- err == nil && time.Since(raised) < time.Second
	}()
	return done
}

func (b *liveAskBoard) askRef() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ref
}

// end answers or withdraws the ask through the store.
func (b *liveAskBoard) end(withdraw bool) error {
	ctx := context.Background()
	if withdraw {
		_, err := b.s.Withdraw(ctx, audit.Actor{Handle: "claude@host"}, "key:x", b.askRef(), "moved on")
		return err
	}
	_, err := b.s.Answer(ctx, audit.Actor{Handle: "nik@host", Operator: true}, "key:x", b.askRef(), asks.Reply{Answer: "8080"})
	return err
}

// runLivePoll runs `board ask aa-1 "Which port?" --poll 1s` against b, and
// returns its exit code and output and how long it took.
func runLivePoll(b *liveAskBoard) (int, string, time.Duration) {
	srv := httptest.NewServer(b)
	defer srv.Close()
	started := time.Now()
	code, out, _ := runEnv((&fakeBoard{}).env(srv), "ask", "aa-1", "Which port?", "--poll", "1s")
	return code, out, time.Since(started)
}

// An answer or withdrawal committed before the deadline ends `board ask --poll
// 1s` at once with exit 0 or 6, on the real clock against the real store.
func TestAskPollLiveCountsAnEndCommittedBeforeTheDeadline(t *testing.T) {
	for _, c := range []struct {
		withdraw bool
		code     int
		out      string
	}{
		{false, ExitOK, "ok A1 answered by nik@host: 8080\n"},
		{true, ExitConflict, "A1 withdrawn: moved on\n"},
	} {
		b := newLiveAskBoard(t)
		done := make(chan error, 1)
		go func() {
			for b.askRef() == "" {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(300 * time.Millisecond)
			done <- b.end(c.withdraw)
		}()
		code, out, took := runLivePoll(b)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if code != c.code || out != c.out {
			t.Errorf("withdraw=%v: got %d %q after %s, want %d %q", c.withdraw, code, out, took, c.code, c.out)
		}
	}
}

// An answer or withdrawal whose transaction takes its stamp before the deadline
// but commits after it leaves the ask open at the deadline: the poll exits 3,
// though a read after the deadline finds the end with a stamp before it.
func TestAskPollLiveEndCommittedAfterTheDeadlineExitsThree(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		b := newLiveAskBoard(t)
		var armed atomic.Bool
		sampled, release := make(chan time.Time, 1), make(chan struct{})
		b.s.Now = func() time.Time {
			now := time.Now()
			if armed.CompareAndSwap(true, false) { // the end's stamp: hold its transaction open
				sampled <- now
				<-release
			}
			return now
		}
		var endErr error
		done := make(chan error, 1)
		var after sync.WaitGroup
		after.Add(1)
		begin := time.Now() // the deadline is a second after the raise, which is after this
		go func() {
			defer after.Done()
			for b.askRef() == "" {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(500 * time.Millisecond) // half way to the deadline
			armed.Store(true)
			go func() { done <- b.end(withdraw) }()
			stamp := <-sampled
			time.Sleep(1100*time.Millisecond - time.Since(begin)) // past the deadline
			if k, err := b.s.Get(context.Background(), b.askRef()); err != nil || k.State != "open" {
				t.Errorf("after the deadline the ask is %q (%v), want still open", k.State, err)
			}
			close(release)
			endErr = <-done // committed after the deadline, its stamp from before it
			if !stamp.Before(begin.Add(time.Second)) {
				t.Errorf("withdraw=%v: the stamp %s is not before the deadline", withdraw, stamp)
			}
		}()
		code, out, took := runLivePoll(b)
		after.Wait()
		if endErr != nil {
			t.Fatal(endErr)
		}
		if code != ExitTimeout || out != "A1 still open → board ask show A1\n" || took < time.Second {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 at the deadline", withdraw, code, out, took)
		}
		if k, _ := b.s.Get(context.Background(), b.askRef()); k.State == "open" {
			t.Errorf("withdraw=%v: the end was never committed", withdraw)
		}
	}
}

// An answer or withdrawal committed before the deadline whose held wait is
// answered only after it (the response is delayed) is still exit 0 or 6: the
// plain read before the deadline finds it, whatever the delay and whether the
// end came early or late in the poll.
func TestAskPollLiveEndCommittedBeforeTheDeadlineWithADelayedWaitResponse(t *testing.T) {
	for _, c := range []struct {
		withdraw bool
		code     int
		at       time.Duration // when the end commits, from the raise
	}{
		{false, ExitOK, 50 * time.Millisecond},
		{true, ExitConflict, 50 * time.Millisecond},
		{false, ExitOK, 700 * time.Millisecond},
		{true, ExitConflict, 700 * time.Millisecond},
	} {
		b := newLiveAskBoard(t)
		b.delay = 1100 * time.Millisecond
		go func() {
			for b.askRef() == "" {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(c.at)
			_ = b.end(c.withdraw)
		}()
		code, out, took := runLivePoll(b)
		if code != c.code || took > time.Second {
			t.Errorf("withdraw=%v at %s: got %d %q after %s, want exit %d within the deadline", c.withdraw, c.at, code, out, took, c.code)
		}
	}
}

// An answer or withdrawal committed after the plain read before the deadline
// found the ask open, but still before the deadline, whose held wait is
// answered only after the deadline (the response is delayed), is exit 0 or 6:
// the read after the deadline finds it, and its ended_at proves it took effect
// in time.
func TestAskPollLiveEndCommittedAfterTheProbeBeforeTheDeadline(t *testing.T) {
	for _, c := range []struct {
		withdraw bool
		code     int
		out      string
		at       time.Duration
	}{
		{false, ExitOK, "ok A1 answered by nik@host: 8080\n", 870 * time.Millisecond},
		{true, ExitConflict, "A1 withdrawn: moved on\n", 870 * time.Millisecond},
		{false, ExitOK, "ok A1 answered by nik@host: 8080\n", 950 * time.Millisecond},
		{true, ExitConflict, "A1 withdrawn: moved on\n", 950 * time.Millisecond},
	} {
		b := newLiveAskBoard(t)
		b.delay = 1100 * time.Millisecond
		inTime := b.endAt(c.at, c.withdraw)
		code, out, took := runLivePoll(b)
		if !<-inTime {
			t.Fatalf("withdraw=%v at %s: the end did not commit before the deadline", c.withdraw, c.at)
		}
		if code != c.code || out != c.out || took < time.Second {
			t.Errorf("withdraw=%v at %s: got %d %q after %s, want %d %q at the deadline", c.withdraw, c.at, code, out, took, c.code, c.out)
		}
	}
}

// When every response is delayed past the deadline, plain reads included, an
// end committed early in the poll is still exit 0 or 6: only the read after the
// deadline comes back, and the end's ended_at, not the time the response came
// back, says it took effect in time.
func TestAskPollLiveEveryResponseDelayedCountsAnEndCommittedInTime(t *testing.T) {
	for _, c := range []struct {
		withdraw bool
		code     int
	}{{false, ExitOK}, {true, ExitConflict}} {
		b := newLiveAskBoard(t)
		b.delay, b.plainDelay = 1100*time.Millisecond, 400*time.Millisecond
		inTime := b.endAt(300*time.Millisecond, c.withdraw)
		code, out, took := runLivePoll(b)
		if !<-inTime {
			t.Fatalf("withdraw=%v: the end did not commit before the deadline", c.withdraw)
		}
		if code != c.code || took < 1400*time.Millisecond {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit %d once the delayed read is back", c.withdraw, code, out, took, c.code)
		}
	}
}

// With every response delayed, an end whose transaction takes its stamp before
// the deadline but commits after it is exit 3: the read after the deadline
// finds it, with a stamp before the deadline but an ended_at after it.
func TestAskPollLiveEveryResponseDelayedEndCommittedAfterTheDeadlineExitsThree(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		b := newLiveAskBoard(t)
		b.delay, b.plainDelay = 1100*time.Millisecond, 400*time.Millisecond
		var armed atomic.Bool
		release := make(chan struct{})
		b.s.Now = func() time.Time {
			now := time.Now()
			if armed.CompareAndSwap(true, false) { // the end's stamp: hold its transaction open
				<-release
			}
			return now
		}
		done := make(chan error, 1)
		go func() {
			for b.askRef() == "" {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(500 * time.Millisecond)
			armed.Store(true)
			go func() { done <- b.end(withdraw) }()
			b.mu.Lock()
			raised := b.raisedAt
			b.mu.Unlock()
			time.Sleep(time.Until(raised.Add(1050 * time.Millisecond))) // past the deadline, before the last read
			close(release)
		}()
		code, out, took := runLivePoll(b)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		k, err := b.s.Get(context.Background(), b.askRef())
		if err != nil || k.EndedAt == nil || !k.EndedAt.After(b.raisedAt.Add(time.Second)) {
			t.Fatalf("withdraw=%v: ended_at %v (%v), want after the deadline", withdraw, k.EndedAt, err)
		}
		if code != ExitTimeout || out != "A1 still open → board ask show A1\n" || took < time.Second {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit 3", withdraw, code, out, took)
		}
	}
}
