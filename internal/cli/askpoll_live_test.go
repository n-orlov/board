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
// open or the client goes, and a plain read, which runs plain first.
type liveAskBoard struct {
	t     *testing.T
	s     *asks.Store
	mu    sync.Mutex
	ref   string
	plain func()
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
		b.ref = k.Ref
		b.mu.Unlock()
		b.write(w, http.StatusCreated, k)
		return
	}
	if r.URL.Query().Get("wait") == "" && b.plain != nil {
		b.plain()
	}
	for {
		k, err := b.s.Get(ctx, b.askRef())
		if err != nil || k.State != "open" || r.URL.Query().Get("wait") == "" {
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
// returns its exit code and output, when it started and how long it took.
func runLivePoll(b *liveAskBoard) (int, string, time.Time, time.Duration) {
	srv := httptest.NewServer(b)
	defer srv.Close()
	started := time.Now()
	code, out, _ := runEnv((&fakeBoard{}).env(srv), "ask", "aa-1", "Which port?", "--poll", "1s")
	return code, out, started, time.Since(started)
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
		code, out, _, took := runLivePoll(b)
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
// though the read after the deadline finds the end with a stamp before it.
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
		var stamp time.Time
		var endErr error
		done := make(chan error, 1)
		b.plain = func() { // the read after the deadline cut the wait off
			time.Sleep(75 * time.Millisecond)
			if k, err := b.s.Get(context.Background(), b.askRef()); err != nil || k.State != "open" {
				t.Errorf("after the deadline the ask is %q (%v), want still open", k.State, err)
			}
			stamp = <-sampled
			close(release)
			endErr = <-done // committed: this read finds the end
		}
		go func() {
			for b.askRef() == "" {
				time.Sleep(time.Millisecond)
			}
			time.Sleep(500 * time.Millisecond) // half way to the deadline
			armed.Store(true)
			done <- b.end(withdraw)
		}()
		code, out, started, took := runLivePoll(b)
		if endErr != nil {
			t.Fatal(endErr)
		}
		if code != ExitTimeout || out != "A1 still open → board ask show A1\n" || took < time.Second {
			t.Errorf("withdraw=%v: got %d %q after %s, want exit 3 at the deadline", withdraw, code, out, took)
		}
		if k, _ := b.s.Get(context.Background(), b.askRef()); k.State == "open" || stamp.IsZero() || !stamp.Before(started.Add(time.Second)) {
			t.Errorf("withdraw=%v: the end was not committed after a stamp taken before the deadline: %q stamp %s", withdraw, k.State, stamp)
		}
	}
}
