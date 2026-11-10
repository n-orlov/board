package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/feed"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/server"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/tasks"
)

// liveAskBoard is the real server over a real SQLite store, in real time:
// `board ask --poll` raises and waits through it, while the test answers or
// withdraws the ask straight through the store (w), as another server on the
// same database would. The change feed is never woken by those writes (it
// polls once an hour), so the server learns of an end only by reading the ask:
// at the start of a wait, or as the wait's until passes.
type liveAskBoard struct {
	t     *testing.T
	st    *store.Store
	reads *asks.Store // the server's
	w     *asks.Store // the ends', with clock as its clock
	srv   *httptest.Server
	mu    sync.Mutex
	// delay holds back every response to a read of the ask this long (not the
	// raise's, so the deadline stays a second after the raise)
	delay time.Duration
	// holdTx, when set, holds the end's transaction open from its first clock
	// read until it is closed, every read in it giving the time of the first
	holdTx chan struct{}
	frozen time.Time
	// postDelay delays the end's first clock read after its commit this long
	postDelay time.Duration
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
	me := audit.Actor{Handle: "claude@host"}
	if _, err := (&initiatives.Store{Store: st}).Create(context.Background(), me, "key:x", initiatives.New{Slug: "aa", Title: "I"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&tasks.Store{Store: st}).Create(context.Background(), me, "key:x", tasks.New{Initiative: "aa", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f, err := feed.New(ctx, feed.StoreSource{Store: st}, feed.Options{Poll: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	go f.Run(ctx)
	b := &liveAskBoard{t: t, st: st, reads: &asks.Store{Store: st}}
	b.w = &asks.Store{Store: st, Now: b.clock}
	verify := func(context.Context, string) (string, error) { return "key:x", nil }
	h := server.NewHandler(func(context.Context) error { return nil }, verify, nil, nil,
		server.WithAsks(b.reads), server.WithStream(f, time.Minute))
	b.srv = httptest.NewServer(b.delayed(h))
	t.Cleanup(b.srv.Close)
	return b
}

// delayed serves h, holding every response to a GET back by b.delay once h is
// done.
func (b *liveAskBoard) delayed(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		b.mu.Lock()
		d := b.delay
		b.mu.Unlock()
		if r.Method != http.MethodGet {
			d = 0
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(d):
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
}

// clock is the clock of the store the ends are written through (see holdTx and
// postDelay).
func (b *liveAskBoard) clock() time.Time {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state() != "open" { // read after the end committed
		if d := b.postDelay; d > 0 {
			b.postDelay = 0
			b.mu.Unlock()
			time.Sleep(d)
			b.mu.Lock()
			return time.Now()
		}
		return now
	}
	if hold := b.holdTx; hold != nil {
		b.holdTx, b.frozen = nil, now
		b.mu.Unlock()
		<-hold
		b.mu.Lock()
	}
	if !b.frozen.IsZero() {
		return b.frozen
	}
	return now
}

// state is the committed state of the first ask, as a reader sees it.
func (b *liveAskBoard) state() string {
	k, err := b.reads.Get(context.Background(), "A1")
	if err != nil {
		return "open"
	}
	return k.State
}

// end answers or withdraws the ask through the store, at after from start.
func (b *liveAskBoard) end(start time.Time, after time.Duration, withdraw bool) error {
	time.Sleep(time.Until(start.Add(after)))
	ctx := context.Background()
	if withdraw {
		_, err := b.w.Withdraw(ctx, audit.Actor{Handle: "claude@host"}, "key:x", "A1", "moved on")
		return err
	}
	_, err := b.w.Answer(ctx, audit.Actor{Handle: "nik@host", Operator: true}, "key:x", "A1", asks.Reply{Answer: "8080"})
	return err
}

// endAt ends the ask at after from start in the background, and reports the
// end's error once it returns.
func (b *liveAskBoard) endAt(start time.Time, after time.Duration, withdraw bool) <-chan error {
	done := make(chan error, 1)
	go func() { done <- b.end(start, after, withdraw) }()
	return done
}

// stateAt reports the committed state of the ask at after from start.
func (b *liveAskBoard) stateAt(start time.Time, after time.Duration) <-chan string {
	out := make(chan string, 1)
	go func() {
		time.Sleep(time.Until(start.Add(after)))
		out <- b.state()
	}()
	return out
}

// runLivePoll runs `board ask aa-1 "Which port?" --poll 1s` against b, and
// returns its exit code and output and how long it took. The poll's deadline is
// a second after the raise returns, so it is later than start plus a second.
func (b *liveAskBoard) runLivePoll() (int, string, time.Duration) {
	started := time.Now()
	code, out, _ := runEnv((&fakeBoard{}).env(b.srv), "ask", "aa-1", "Which port?", "--poll", "1s")
	return code, out, time.Since(started)
}

// pollCase is the expected outcome of a poll the ask is answered or withdrawn in.
type pollCase struct {
	withdraw bool
	code     int
	out      string
}

var endCases = []pollCase{
	{false, ExitOK, "ok A1 answered by nik@host: 8080\n"},
	{true, ExitConflict, "A1 withdrawn: moved on\n"},
}

const stillOpen = "A1 still open → board ask show A1\n"

// An answer or withdrawal committed before the deadline ends `board ask --poll
// 1s` with exit 0 or 6, on the real clock against the real server and store.
func TestAskPollLiveCountsAnEndCommittedBeforeTheDeadline(t *testing.T) {
	for _, c := range endCases {
		b := newLiveAskBoard(t)
		start := time.Now()
		done := b.endAt(start, 300*time.Millisecond, c.withdraw)
		code, out, took := b.runLivePoll()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if code != c.code || out != c.out {
			t.Errorf("withdraw=%v: got %d %q after %s, want %d %q", c.withdraw, code, out, took, c.code, c.out)
		}
	}
}

// An end committed before the deadline is exit 0 or 6 however late the
// server's answer reaches the CLI: every response is held back past the
// deadline, so the CLI observes the end only after it.
func TestAskPollLiveCountsAnEndCommittedInTimeButObservedLate(t *testing.T) {
	for _, at := range []time.Duration{300 * time.Millisecond, 950 * time.Millisecond} {
		for _, c := range endCases {
			b := newLiveAskBoard(t)
			b.delay = 1100 * time.Millisecond
			start := time.Now()
			done := b.endAt(start, at, c.withdraw)
			committed := b.stateAt(start, time.Second) // before the deadline
			code, out, took := b.runLivePoll()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if s := <-committed; s == "open" {
				t.Fatalf("withdraw=%v at %s: the end had not committed before the deadline", c.withdraw, at)
			}
			if code != c.code || out != c.out || took < 2*time.Second {
				t.Errorf("withdraw=%v at %s: got %d %q after %s, want %d %q once the delayed answer is back", c.withdraw, at, code, out, took, c.code, c.out)
			}
		}
	}
}

// An end committed before the deadline whose clock read after the commit is
// delayed well past the deadline (so its ended_at is after it, and nothing the
// server sends before the deadline reports the end) is still exit 0 or 6: what
// counts is the commit, before the deadline, not when anything later about it
// was stamped or observed. With and without delayed responses.
func TestAskPollLiveCountsAnEndWhosePostCommitClockReadIsDelayed(t *testing.T) {
	for _, delay := range []time.Duration{0, 400 * time.Millisecond} {
		for _, c := range endCases {
			b := newLiveAskBoard(t)
			b.delay, b.postDelay = delay, 1200*time.Millisecond
			start := time.Now()
			done := b.endAt(start, 880*time.Millisecond, c.withdraw)
			committed := b.stateAt(start, time.Second)
			code, out, took := b.runLivePoll()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if s := <-committed; s == "open" {
				t.Fatalf("withdraw=%v delay %s: the end had not committed before the deadline", c.withdraw, delay)
			}
			k, err := b.reads.Get(context.Background(), "A1")
			if err != nil || k.EndedAt == nil || !k.EndedAt.After(start.Add(time.Second+delay)) {
				t.Fatalf("withdraw=%v delay %s: ended_at %v (%v), want after the deadline", c.withdraw, delay, k.EndedAt, err)
			}
			if code != c.code || out != c.out {
				t.Errorf("withdraw=%v delay %s: got %d %q after %s, want %d %q", c.withdraw, delay, code, out, took, c.code, c.out)
			}
		}
	}
}

// An answer or withdrawal whose transaction takes its stamp before the deadline
// but commits after it leaves the ask open at the deadline: the poll exits 3,
// though the end is committed, stamped before the deadline, by the time the
// server's answer reaches the CLI. With and without delayed responses.
func TestAskPollLiveEndCommittedAfterTheDeadlineExitsThree(t *testing.T) {
	for _, delay := range []time.Duration{0, 400 * time.Millisecond} {
		for _, c := range endCases {
			b := newLiveAskBoard(t)
			b.delay = delay
			release := make(chan struct{})
			b.holdTx = release
			start := time.Now()
			done := b.endAt(start, 500*time.Millisecond, c.withdraw)
			atDeadline := b.stateAt(start, 1050*time.Millisecond)
			go func() {
				time.Sleep(time.Until(start.Add(1150 * time.Millisecond)))
				close(release)
			}()
			code, out, took := b.runLivePoll()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if s := <-atDeadline; s != "open" {
				t.Fatalf("withdraw=%v delay %s: the ask was %q at the deadline, want still open", c.withdraw, delay, s)
			}
			k, err := b.reads.Get(context.Background(), "A1")
			stamp := k.AnsweredAt
			if c.withdraw {
				stamp = k.WithdrawnAt
			}
			if err != nil || k.State == "open" || stamp == nil || !stamp.Before(start.Add(time.Second)) {
				t.Fatalf("withdraw=%v delay %s: the end is %q stamped %v (%v), want committed and stamped before the deadline", c.withdraw, delay, k.State, stamp, err)
			}
			if code != ExitTimeout || out != stillOpen || took < time.Second {
				t.Errorf("withdraw=%v delay %s: got %d %q after %s, want exit 3 at the deadline", c.withdraw, delay, code, out, took)
			}
		}
	}
}

// An end committed before the deadline counts while a later write holds the
// database's writer across the deadline: the server's read as the deadline
// passes does not wait for the writer, and a write committed after the deadline
// does not decide it either way.
func TestAskPollLiveCountsAnEndWhileAWriteHoldsTheWriterAcrossTheDeadline(t *testing.T) {
	for _, c := range endCases {
		b := newLiveAskBoard(t)
		b.delay = 300 * time.Millisecond
		start := time.Now()
		done := b.endAt(start, 800*time.Millisecond, c.withdraw)
		// a second ask, raised after the poll's, is answered at 900 ms in a
		// transaction held to 1300 ms
		holder := &asks.Store{Store: b.st, Now: func() time.Time {
			now := time.Now()
			if now.Before(start.Add(time.Second)) {
				time.Sleep(time.Until(start.Add(1300 * time.Millisecond)))
			}
			return now
		}}
		held := make(chan error, 1)
		go func() {
			time.Sleep(time.Until(start.Add(600 * time.Millisecond)))
			other, err := b.reads.Raise(context.Background(), audit.Actor{Handle: "claude@host"}, "key:x", asks.New{Question: "Other?"})
			if err == nil {
				time.Sleep(time.Until(start.Add(900 * time.Millisecond)))
				_, err = holder.Answer(context.Background(), audit.Actor{Handle: "nik@host", Operator: true}, "key:x", other.Ref, asks.Reply{Answer: "x"})
			}
			held <- err
		}()
		code, out, took := b.runLivePoll()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := <-held; err != nil {
			t.Fatal(err)
		}
		if code != c.code || out != c.out || took < 1300*time.Millisecond {
			t.Errorf("withdraw=%v: got %d %q after %s, want %d %q once the writer is free", c.withdraw, code, out, took, c.code, c.out)
		}
	}
}

// An ask nobody ends is exit 3 once the whole second has passed, with no answer
// read before the deadline.
func TestAskPollLiveOpenAtTheDeadlineExitsThree(t *testing.T) {
	b := newLiveAskBoard(t)
	code, out, took := b.runLivePoll()
	if code != ExitTimeout || out != stillOpen || took < time.Second || took > 3*time.Second {
		t.Errorf("got %d %q after %s, want exit 3 at the deadline", code, out, took)
	}
}
