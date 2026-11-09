package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/feed"
	"github.com/n-orlov/board/internal/store"
)

// testClock is a clock the test moves.
type testClock struct{ ms atomic.Int64 }

func newTestClock() *testClock {
	c := &testClock{}
	c.ms.Store(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).UnixMilli())
	return c
}
func (c *testClock) Now() time.Time          { return time.UnixMilli(c.ms.Load()).UTC() }
func (c *testClock) Advance(d time.Duration) { c.ms.Add(d.Milliseconds()) }

// askRig is a rig that serves asks from a store with the clock, its ask waits
// renewing poll_seen_at every touch.
func askRig(t *testing.T, st *store.Store, token string, clock *testClock, touch time.Duration) (*rig, *asks.Store) {
	t.Helper()
	as := &asks.Store{Store: st, Now: clock.Now}
	cfg := streamSettings{poll: 100 * time.Millisecond, heartbeat: time.Minute, extra: func(*store.Store) []Option {
		return []Option{WithAsks(as), WithAskTouch(touch)}
	}}
	return startRig(t, st, token, cfg), as
}

// raiseAsk raises a poll-mode global ask through the server and returns its ref.
func (r *rig) raiseAsk() string {
	r.t.Helper()
	resp := r.do(http.MethodPost, "/api/v1/asks", `{"question":"Which one?","mode":"poll"}`, r.token)
	defer func() { _ = resp.Body.Close() }()
	var doc struct{ Ref string }
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil || resp.StatusCode != http.StatusCreated {
		r.t.Fatalf("raise = %d, %v", resp.StatusCode, err)
	}
	return doc.Ref
}

// heldWait is a GET with ?wait= running in the background.
type heldWait struct {
	status int
	state  string
	done   chan struct{}
}

func (r *rig) startWait(ref string, seconds string) *heldWait {
	r.t.Helper()
	w := &heldWait{done: make(chan struct{})}
	go func() {
		defer close(w.done)
		resp := r.do(http.MethodGet, "/api/v1/asks/"+ref+"?wait="+seconds, "", r.token)
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		var doc struct{ State string }
		_ = json.Unmarshal(b, &doc)
		w.status, w.state = resp.StatusCode, doc.State
	}()
	return w
}

func (w *heldWait) ends(t *testing.T) {
	t.Helper()
	const within = 5 * time.Second
	select {
	case <-w.done:
	case <-time.After(within):
		t.Fatalf("the wait was still held after %s", within)
	}
}

func (r *rig) answer(ref string) {
	r.t.Helper()
	resp := r.do(http.MethodPost, "/api/v1/asks/"+ref+"/answer", `{"answer":"yes"}`, r.token)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.t.Fatalf("answer = %d", resp.StatusCode)
	}
}

// polledAt reads poll_seen_at of the ask straight from the store.
func polledAt(t *testing.T, as *asks.Store, ref string) *time.Time {
	t.Helper()
	k, err := as.Get(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return k.PollSeenAt
}

// eventually polls cond for up to 5 seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within 5 s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAWaitSetsPollSeenAtAtItsStartAndAtItsEnd(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock := newTestClock()
		// A touch interval of an hour: only the start and the end write.
		r, as := askRig(t, st, newToken(t, st), clock, time.Hour)
		ref := r.raiseAsk()
		if polledAt(t, as, ref) != nil {
			t.Fatal("poll_seen_at is set before any wait")
		}
		start := clock.Now()
		w := r.startWait(ref, "1")
		eventually(t, "poll_seen_at at the start", func() bool { p := polledAt(t, as, ref); return p != nil && p.Equal(start) })
		clock.Advance(7 * time.Second)
		end := clock.Now()
		w.ends(t)
		if w.status != http.StatusOK || w.state != "open" {
			t.Errorf("the wait ended with %d %q, want 200 open", w.status, w.state)
		}
		if p := polledAt(t, as, ref); p == nil || !p.Equal(end) {
			t.Errorf("poll_seen_at after the wait = %v, want the end of the wait %v", p, end)
		}
	})
}

func TestAHeldWaitRenewsPollSeenAtOnEveryInterval(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock := newTestClock()
		r, as := askRig(t, st, newToken(t, st), clock, 20*time.Millisecond)
		ref := r.raiseAsk()
		w := r.startWait(ref, "30")
		first := clock.Now()
		eventually(t, "poll_seen_at at the start", func() bool { return polledAt(t, as, ref) != nil })
		for i := 1; i <= 3; i++ {
			clock.Advance(10 * time.Second)
			want := clock.Now()
			eventually(t, "a renewal of poll_seen_at", func() bool { p := polledAt(t, as, ref); return p != nil && !p.Before(want) })
		}
		select {
		case <-w.done:
			t.Fatal("the wait ended while the ask was open")
		default:
		}
		if p := polledAt(t, as, ref); !p.After(first) {
			t.Errorf("poll_seen_at = %v, want later than %v", p, first)
		}
		r.answer(ref)
		w.ends(t)
		if w.state != "answered" {
			t.Errorf("state after the answer = %q", w.state)
		}
	})
}

func TestAWaitEndsWhenTheClientLeavesAndMarksThePoll(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock := newTestClock()
		r, as := askRig(t, st, newToken(t, st), clock, time.Hour)
		ref := r.raiseAsk()
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.base+"/api/v1/asks/"+ref+"?wait=60", nil)
		req.Header.Set("Authorization", "Bearer "+r.token)
		req.Header.Set(ActorHeader, "tester@host")
		done := make(chan struct{})
		go func() {
			defer close(done)
			if resp, err := noKeepAlive.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
		eventually(t, "poll_seen_at at the start", func() bool { return polledAt(t, as, ref) != nil })
		clock.Advance(3 * time.Second)
		left := clock.Now()
		cancel()
		<-done
		eventually(t, "poll_seen_at at the end of the abandoned wait", func() bool { p := polledAt(t, as, ref); return p != nil && p.Equal(left) })
	})
}

func TestAWaitHeldOnOneServerEndsWhenTheAnswerIsWrittenThroughAnotherOnOnePostgres(t *testing.T) {
	url := startPostgres(t).New(t)
	a := openStore(t, url)
	if err := a.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	token := newToken(t, a)
	clock := newTestClock()
	writer, _ := askRig(t, a, token, clock, time.Hour)
	reader, as := askRig(t, openStore(t, url), token, clock, time.Hour)
	ref := writer.raiseAsk()
	w := reader.startWait(ref, "120")
	eventually(t, "the wait to be held", func() bool { return polledAt(t, as, ref) != nil })
	writer.answer(ref)
	// The reader's feed tails every 100 ms: the wait ends within a few of them, not 120 s.
	w.ends(t)
	if w.status != http.StatusOK || w.state != "answered" {
		t.Errorf("the wait on the other server ended with %d %q, want 200 answered", w.status, w.state)
	}
}

func TestAWaitIsRefusedOutsideOneToThreeHundredAndAnswers503WithoutAFeed(t *testing.T) {
	o := askOps{asks: &asks.Store{}}
	for _, n := range []int{0, 301, -5} {
		got, err := o.GetAsk(context.Background(), GetAskRequestObject{Ref: "A1", Params: GetAskParams{Wait: &n}})
		if p, ok := got.(problemResponse); err != nil || !ok || p.status != http.StatusBadRequest {
			t.Errorf("wait=%d = %#v, %v, want a 400 problem", n, got, err)
		}
	}
	for _, n := range []int{1, 300} {
		if p := askWaitProblem(&n); p != nil {
			t.Errorf("wait=%d refused: %+v", n, p)
		}
	}
	if askWaitProblem(nil) != nil {
		t.Error("no wait refused")
	}
}

func TestAWaitEndsWithTheOpenAskWhenTheServerShutsDown(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		r, as := askRig(t, st, newToken(t, st), newTestClock(), time.Hour)
		ref := r.raiseAsk()
		w := r.startWait(ref, "120")
		eventually(t, "the wait to be held", func() bool { return polledAt(t, as, ref) != nil })
		if err := r.stop(); err != nil {
			t.Errorf("the server stopped with %v", err)
		}
		w.ends(t)
		if w.status != http.StatusOK || w.state != "open" {
			t.Errorf("the wait ended by the shutdown = %d %q, want 200 open", w.status, w.state)
		}
	})
}

func TestAWaitStartsOverWhenTheFeedEndsItsSubscription(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock := newTestClock()
		as := &asks.Store{Store: st, Now: clock.Now}
		r, _ := askRig(t, st, newToken(t, st), clock, time.Hour)
		ref := r.raiseAsk()
		// A waiter of its own over a feed that is never run, so the test ends its subscription.
		f := feed.NewAt(nil, 0, feed.Options{})
		hub := &streamHub{feed: f, heartbeat: time.Hour, stop: make(chan struct{})}
		k, err := as.Get(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		w := &waiter{ctx: context.Background(), store: as, hub: hub, k: k, sub: f.Subscribe()}
		w.sub.Close() // the feed ended it: the channel is closed
		if done, err := w.step(nil, nil); done || err != nil {
			t.Fatalf("step on an ended subscription = %v, %v, want to go on", done, err)
		}
		select {
		case _, open := <-w.sub.Events():
			if !open {
				t.Fatal("the waiter kept the ended subscription")
			}
		default:
		}
		w.sub.Close()
	})
}

func TestAWaitOnAnOpenAskWithoutAFeedAnswers503AndOnAnAnsweredOneAnswersAtOnce(t *testing.T) {
	wait := 5
	o := askOps{asks: &asks.Store{}}
	got := o.read(context.Background(), asks.Ask{State: "open"}, &wait)
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("a wait without a feed = %#v, want a 503 problem", got)
	}
	got = o.read(context.Background(), asks.Ask{State: "answered"}, &wait)
	if _, ok := got.(GetAsk200JSONResponse); !ok {
		t.Errorf("a wait on an answered ask = %#v, want the ask at once", got)
	}
	got = o.read(context.Background(), asks.Ask{State: "open"}, nil)
	if _, ok := got.(GetAsk200JSONResponse); !ok {
		t.Errorf("an open ask without wait = %#v, want the ask at once", got)
	}
}
