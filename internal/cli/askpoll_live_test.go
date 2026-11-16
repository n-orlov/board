package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/feed"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/server"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// liveAskBoard is the real server over a real SQLite store, in real time:
// `vh ask --poll` raises and waits through it, while the test answers or
// withdraws the ask straight through the store (w), as another server on the
// same database would. The change feed is never woken by those writes (it
// polls once an hour), so the server learns of an end only by reading the ask:
// at the start of a wait, or as the wait's until passes.
type liveAskBoard struct {
	t     *testing.T
	path  string // the database file
	st    *store.Store
	reads *asks.Store // the server's
	w     *asks.Store // the ends', with clock as its clock
	srv   *httptest.Server
	mu    sync.Mutex
	// delay holds back every response to a read of the ask this long (not the
	// raise's, so the deadline stays a second after the raise)
	delay time.Duration
	// early holds back every read of the ask this long before the server
	// handles it, so the end can take the writer first
	early time.Duration
	// holdTx, when set, holds the end's transaction open from its first clock
	// read until it is closed, every read in it giving the time of the first
	holdTx chan struct{}
	frozen time.Time
	// postDelay delays the end's first clock read after its commit this long
	postDelay time.Duration
	// onGet, when set, runs as every read of the ask reaches the server
	onGet func(r *http.Request)
}

// liveSlots caps how many live boards run at once. The live tests run in
// parallel, but each judges what happened by margins of 50 ms or so on the
// real clock, so only a few share the host at a time: enough to overlap their
// sleeps, too few to starve one another's timers (under -race especially).
var (
	liveSlots = make(chan struct{}, 4)
	liveHeld  sync.Map // *testing.T -> its slot is taken
)

// takeLiveSlot waits for a free slot for t, held until t ends. A test that
// opens several boards (a retried poll) takes one slot for all of them.
func takeLiveSlot(t *testing.T) {
	if _, held := liveHeld.LoadOrStore(t, true); held {
		return
	}
	liveSlots <- struct{}{}
	t.Cleanup(func() {
		liveHeld.Delete(t)
		<-liveSlots
	})
}

func newLiveAskBoard(t *testing.T) *liveAskBoard {
	takeLiveSlot(t)
	path := filepath.Join(t.TempDir(), "b.db")
	st, err := store.Open("sqlite://" + path)
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
	b := &liveAskBoard{t: t, path: path, st: st, reads: &asks.Store{Store: st}}
	b.w = &asks.Store{Store: st, Now: b.clock}
	verify := func(context.Context, string) (string, error) { return "key:x", nil }
	h := server.NewHandler(func(context.Context) error { return nil }, verify, nil, nil,
		server.WithAsks(b.reads), server.WithStream(f, time.Minute))
	b.srv = httptest.NewServer(b.delayed(h))
	t.Cleanup(b.srv.Close)
	b.warmReaders()
	return b
}

// warmReaders opens the server's whole read pool and keeps it idle, so
// hogReaders takes the connections at the moment it is told to rather than
// opening them one by one then: under -race on a many-core host that takes
// longer than the window the readers are to be taken for.
func (b *liveAskBoard) warmReaders() {
	pool := b.st.Reader()
	n := pool.Stats().MaxOpenConnections
	pool.SetMaxIdleConns(n)
	conns := make([]*sql.Conn, 0, n)
	for range n {
		c, err := pool.Conn(context.Background())
		if err != nil {
			b.t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

// delayed serves h, holding every response to a GET back by b.delay once h is
// done.
func (b *liveAskBoard) delayed(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		d, early, onGet := b.delay, b.early, b.onGet
		b.mu.Unlock()
		if r.Method == http.MethodGet && onGet != nil {
			onGet(r)
		}
		if r.Method == http.MethodGet && early > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(early):
			}
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
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

// runLivePoll runs `vh ask aa-1 "Which port?" --poll 1s` against b, and
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

const stillOpen = "A1 still open → vh ask show A1\n"

// An answer or withdrawal committed before the deadline ends `vh ask --poll
// 1s` with exit 0 or 6, on the real clock against the real server and store.
func TestAskPollLiveCountsAnEndCommittedBeforeTheDeadline(t *testing.T) {
	t.Parallel()
	for _, c := range endCases {
		t.Run(fmt.Sprintf("withdraw=%v", c.withdraw), func(t *testing.T) {
			t.Parallel()
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
		})
	}
}

// An end committed before the deadline is exit 0 or 6 however late the
// server's answer reaches the CLI: every response is held back past the
// deadline, so the CLI observes the end only after it.
func TestAskPollLiveCountsAnEndCommittedInTimeButObservedLate(t *testing.T) {
	t.Parallel()
	for _, at := range []time.Duration{300 * time.Millisecond, 950 * time.Millisecond} {
		for _, c := range endCases {
			t.Run(fmt.Sprintf("withdraw=%v at=%s", c.withdraw, at), func(t *testing.T) {
				t.Parallel()
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
			})
		}
	}
}

// An end committed before the deadline whose clock read after the commit is
// delayed well past the deadline (so its ended_at is after it, and nothing the
// server sends before the deadline reports the end) is still exit 0 or 6: what
// counts is the commit, before the deadline, not when anything later about it
// was stamped or observed. With and without delayed responses.
func TestAskPollLiveCountsAnEndWhosePostCommitClockReadIsDelayed(t *testing.T) {
	t.Parallel()
	for _, delay := range []time.Duration{0, 400 * time.Millisecond} {
		for _, c := range endCases {
			t.Run(fmt.Sprintf("withdraw=%v delay=%s", c.withdraw, delay), func(t *testing.T) {
				t.Parallel()
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
			})
		}
	}
}

// An answer or withdrawal whose transaction takes its stamp before the deadline
// but commits after it leaves the ask open at the deadline: the poll exits 3,
// though the end is committed, stamped before the deadline, by the time the
// server's answer reaches the CLI. With and without delayed responses.
func TestAskPollLiveEndCommittedAfterTheDeadlineExitsThree(t *testing.T) {
	t.Parallel()
	for _, delay := range []time.Duration{0, 400 * time.Millisecond} {
		for _, c := range endCases {
			t.Run(fmt.Sprintf("withdraw=%v delay=%s", c.withdraw, delay), func(t *testing.T) {
				t.Parallel()
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
			})
		}
	}
}

// An answer or withdrawal whose transaction already holds the writer when the
// poll's first read reaches the server (so the server's write marking the poll
// waits for it) and commits after the deadline leaves the ask open at the
// deadline: exit 3 with the still-open line, the deadline sampled on its own
// timer and not once that first write is let through. With the responses held
// back too, and with the end taking the writer before the poll even starts.
func TestAskPollLiveEndHoldingTheWriterBeforeThePollReachesTheServerExitsThree(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ early, delay, endAt time.Duration }{
		{300 * time.Millisecond, 0, 100 * time.Millisecond},
		{300 * time.Millisecond, 400 * time.Millisecond, 100 * time.Millisecond},
		{100 * time.Millisecond, 0, 20 * time.Millisecond},
	} {
		for _, e := range endCases {
			t.Run(fmt.Sprintf("early=%s delay=%s end=%s withdraw=%v", c.early, c.delay, c.endAt, e.withdraw), func(t *testing.T) {
				t.Parallel()
				b := newLiveAskBoard(t)
				b.early, b.delay = c.early, c.delay
				release := make(chan struct{})
				b.holdTx = release
				start := time.Now()
				done := b.endAt(start, c.endAt, e.withdraw)
				atDeadline := b.stateAt(start, 1050*time.Millisecond)
				go func() {
					time.Sleep(time.Until(start.Add(1300 * time.Millisecond)))
					close(release)
				}()
				code, out, took := b.runLivePoll()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if s := <-atDeadline; s != "open" {
					t.Fatalf("%+v withdraw=%v: the ask was %q at the deadline, want still open", c, e.withdraw, s)
				}
				if k, err := b.reads.Get(context.Background(), "A1"); err != nil || k.State == "open" {
					t.Fatalf("%+v withdraw=%v: the end did not commit (%v)", c, e.withdraw, err)
				}
				if code != ExitTimeout || out != stillOpen || took < time.Second {
					t.Errorf("%+v withdraw=%v: got %d %q after %s, want exit 3 at the deadline", c, e.withdraw, code, out, took)
				}
			})
		}
	}
}

// An end committed before the deadline counts while a later write holds the
// database's writer across the deadline: the server's read as the deadline
// passes does not wait for the writer, and a write committed after the deadline
// does not decide it either way.
func TestAskPollLiveCountsAnEndWhileAWriteHoldsTheWriterAcrossTheDeadline(t *testing.T) {
	t.Parallel()
	for _, c := range endCases {
		t.Run(fmt.Sprintf("withdraw=%v", c.withdraw), func(t *testing.T) {
			t.Parallel()
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
		})
	}
}

// An ask nobody ends is exit 3 once the whole second has passed, with no answer
// read before the deadline.
func TestAskPollLiveOpenAtTheDeadlineExitsThree(t *testing.T) {
	t.Parallel()
	b := newLiveAskBoard(t)
	code, out, took := b.runLivePoll()
	if code != ExitTimeout || out != stillOpen || took < time.Second || took > 3*time.Second {
		t.Errorf("got %d %q after %s, want exit 3 at the deadline", code, out, took)
	}
}

// watcher is a second store on b's database, with its own connections, so the
// test can read the committed state of the ask while the server's readers and
// writer are all taken.
func (b *liveAskBoard) watcher() *asks.Store {
	st, err := store.Open("sqlite://" + b.path)
	if err != nil {
		b.t.Fatal(err)
	}
	b.t.Cleanup(func() { _ = st.Close() })
	k := &asks.Store{Store: st}
	// Read once now, so the read the test times later reuses an open
	// connection rather than opening one in its window: opening one takes
	// longer under -race with the other live tests running alongside.
	if _, err := k.Get(context.Background(), "A1"); err != nil && !errors.Is(err, asks.ErrNotFound) {
		b.t.Fatal(err)
	}
	return k
}

// stateBy reports the committed state of the ask at after from start, read
// through k.
func stateBy(k *asks.Store, start time.Time, after time.Duration) <-chan string {
	out := make(chan string, 1)
	go func() {
		time.Sleep(time.Until(start.Add(after)))
		got, err := k.Get(context.Background(), "A1")
		if err != nil {
			out <- "unread: " + err.Error()
			return
		}
		out <- got.State
	}()
	return out
}

// hogReaders takes every connection of the server's read pool from `from` to
// `to` after start, so no read the server makes in between can run.
func (b *liveAskBoard) hogReaders(start time.Time, from, to time.Duration) {
	go func() {
		time.Sleep(time.Until(start.Add(from)))
		pool := b.st.Reader()
		var held []interface{ Close() error }
		for range pool.Stats().MaxOpenConnections {
			c, err := pool.Conn(context.Background())
			if err != nil {
				b.t.Error(err)
				break
			}
			held = append(held, c)
		}
		time.Sleep(time.Until(start.Add(to)))
		for _, c := range held {
			_ = c.Close()
		}
	}()
}

// livePollEnd runs one poll in which the end takes the writer at endAt (its
// transaction then held open, its stamps frozen there) and commits once
// released at release; prep sets up whatever else the case delays. It checks
// the ask was still open at the deadline and committed by the end of the
// poll, and returns the poll's exit code, output and duration.
func livePollEnd(t *testing.T, withdraw bool, endAt, release time.Duration, prep func(b *liveAskBoard, start time.Time)) (int, string, time.Duration) {
	b := newLiveAskBoard(t)
	watch := b.watcher()
	hold := make(chan struct{})
	b.holdTx = hold
	start := time.Now()
	prep(b, start)
	done := b.endAt(start, endAt, withdraw)
	atDeadline := stateBy(watch, start, 1050*time.Millisecond)
	go func() {
		time.Sleep(time.Until(start.Add(release)))
		close(hold)
	}()
	code, out, took := b.runLivePoll()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s := <-atDeadline; s != "open" {
		t.Fatalf("withdraw=%v: the ask was %q at the deadline, want still open", withdraw, s)
	}
	if k, err := watch.Get(context.Background(), "A1"); err != nil || k.State == "open" || k.EndCommitAt == nil {
		t.Fatalf("withdraw=%v: the end did not commit with its mark (%+v, %v)", withdraw, k, err)
	}
	return code, out, took
}

// An end that holds the writer before the poll's first read reaches the
// server, and commits after the deadline, is exit 3 with the still-open line
// even when that first read reaches the server only well after the deadline,
// by which time the end is committed: the server answers with the ask as it
// stood at the deadline, not as its first read finds it.
func TestAskPollLiveQueuedFirstReadDoesNotCountAnEndCommittedAfterTheDeadline(t *testing.T) {
	t.Parallel()
	for _, e := range endCases {
		t.Run(fmt.Sprintf("withdraw=%v", e.withdraw), func(t *testing.T) {
			t.Parallel()
			code, out, took := livePollEnd(t, e.withdraw, 100*time.Millisecond, 1100*time.Millisecond, func(b *liveAskBoard, _ time.Time) {
				b.early = 1600 * time.Millisecond
			})
			if code != ExitTimeout || out != stillOpen || took < 1600*time.Millisecond {
				t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 once the queued read is answered", e.withdraw, code, out, took)
			}
		})
	}
}

// The same end is exit 3 when the poll's first read reaches the server in
// time but every reader of the server is taken from just before the deadline
// to well after the end commits, so the server's read at the deadline runs
// only then and finds the end: when it is read does not decide the answer.
func TestAskPollLiveLateReadAtTheDeadlineDoesNotCountAnEndCommittedAfterIt(t *testing.T) {
	t.Parallel()
	for _, e := range endCases {
		t.Run(fmt.Sprintf("withdraw=%v", e.withdraw), func(t *testing.T) {
			t.Parallel()
			code, out, took := livePollEnd(t, e.withdraw, 100*time.Millisecond, 1100*time.Millisecond, func(b *liveAskBoard, start time.Time) {
				b.early = 200 * time.Millisecond
				b.hogReaders(start, 900*time.Millisecond, 1250*time.Millisecond)
			})
			if code != ExitTimeout || out != stillOpen || took < 1250*time.Millisecond {
				t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 once the readers are free", e.withdraw, code, out, took)
			}
		})
	}
}

// Its counterpart: an end committed before the deadline is exit 0 or 6 when
// the poll's first read reaches the server only after the deadline, or when
// the server's readers are all taken across the deadline, so the server reads
// it late either way.
func TestAskPollLiveCountsAnEndCommittedInTimeThoughEveryServerReadIsLate(t *testing.T) {
	t.Parallel()
	for i, late := range []func(b *liveAskBoard, start time.Time){
		func(b *liveAskBoard, _ time.Time) { b.early = 1600 * time.Millisecond },
		func(b *liveAskBoard, start time.Time) {
			b.early = 200 * time.Millisecond
			b.hogReaders(start, 900*time.Millisecond, 1250*time.Millisecond)
		},
	} {
		for _, e := range endCases {
			t.Run(fmt.Sprintf("late=%d withdraw=%v", i, e.withdraw), func(t *testing.T) {
				t.Parallel()
				b := newLiveAskBoard(t)
				start := time.Now()
				late(b, start)
				done := b.endAt(start, 700*time.Millisecond, e.withdraw)
				code, out, took := b.runLivePoll()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if code != e.code || out != e.out || took < 1200*time.Millisecond {
					t.Errorf("withdraw=%v: got %d %q after %s, want %d %q", e.withdraw, code, out, took, e.code, e.out)
				}
			})
		}
	}
}

// An end's commit mark is persisted after the end commits, and the write of it
// may itself run past the deadline. An end committed in time is exit 0 or 6
// however that write and the server's reads are delayed (readers all taken
// across the deadline, or the first read reaching the server only after it),
// and an end committed after the deadline is exit 3 under the same delays: the
// mark's lateness never moves an end across the deadline in either direction.
func TestAskPollLiveMarkWriteHeldPastTheDeadlineDecidesByWhenTheEndCommitted(t *testing.T) {
	t.Parallel()
	delays := []struct {
		name    string
		release time.Duration // when the held mark write is let go
		late    func(b *liveAskBoard, start time.Time)
	}{
		{"readers taken across the deadline", 1500 * time.Millisecond, func(b *liveAskBoard, start time.Time) {
			b.early = 200 * time.Millisecond
			b.hogReaders(start, 800*time.Millisecond, 1250*time.Millisecond)
		}},
		{"first read after the deadline", 1900 * time.Millisecond, func(b *liveAskBoard, _ time.Time) {
			b.early = 1600 * time.Millisecond
		}},
	}
	for _, d := range delays {
		for _, c := range []struct {
			endAt  time.Duration
			counts bool
		}{{600 * time.Millisecond, true}, {1100 * time.Millisecond, false}} {
			for _, e := range endCases {
				t.Run(fmt.Sprintf("%s withdraw=%v end=%s", d.name, e.withdraw, c.endAt), func(t *testing.T) {
					t.Parallel()
					registerMarkHold(t)
					b := newLiveAskBoard(t)
					hold := b.holdMarkWrites()
					start := time.Now()
					d.late(b, start)
					go func() {
						time.Sleep(time.Until(start.Add(d.release)))
						close(hold.release)
					}()
					done := b.endAt(start, c.endAt, e.withdraw)
					code, out, took := b.runLivePoll()
					if err := <-done; err != nil {
						t.Fatal(err)
					}
					wantCode, wantOut := e.code, e.out
					if !c.counts {
						wantCode, wantOut = ExitTimeout, stillOpen
					}
					if code != wantCode || out != wantOut || took < 1200*time.Millisecond {
						t.Errorf("got %d %q after %s, want %d %q", code, out, took, wantCode, wantOut)
					}
				})
			}
		}
	}
}
