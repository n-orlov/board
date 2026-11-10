package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/feed"
	"github.com/n-orlov/board/internal/store"
	"github.com/oapi-codegen/runtime"
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
	for _, query := range []string{"?wait=60", "?wait=60&until=" + url.QueryEscape(time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))} {
		t.Run(query, func(t *testing.T) { testAWaitEndsWhenTheClientLeaves(t, query) })
	}
}

func testAWaitEndsWhenTheClientLeaves(t *testing.T, query string) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock := newTestClock()
		r, as := askRig(t, st, newToken(t, st), clock, time.Hour)
		ref := r.raiseAsk()
		ctx, cancel := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.base+"/api/v1/asks/"+ref+query, nil)
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
	until := time.Now()
	for _, n := range []int{1, 300} {
		if p := askWaitProblem(&n, nil); p != nil {
			t.Errorf("wait=%d refused: %+v", n, p)
		}
		if p := askWaitProblem(&n, &until); p != nil {
			t.Errorf("wait=%d with until refused: %+v", n, p)
		}
	}
	if askWaitProblem(nil, nil) != nil {
		t.Error("no wait refused")
	}
	got, err := o.GetAsk(context.Background(), GetAskRequestObject{Ref: "A1", Params: GetAskParams{Until: &until}})
	if p, ok := got.(problemResponse); err != nil || !ok || p.status != http.StatusBadRequest || p.next == "" {
		t.Errorf("until without wait = %#v, %v, want a 400 problem with a next", got, err)
	}
}

// untilQuery is ?wait=seconds&until=at.
func untilQuery(seconds string, at time.Time) string {
	return seconds + "&until=" + url.QueryEscape(at.UTC().Format(time.RFC3339Nano))
}

// A wait with until ends at until, sub-second included, with the ask still
// open, when nothing ends it first; until after the wait's own end changes
// nothing; until already past answers at once.
func TestAWaitWithUntilEndsAtUntil(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		testAWaitWithUntilEndsAtUntil(t, openStore(t, dbs.New(t)))
	})
}

func testAWaitWithUntilEndsAtUntil(t *testing.T, st *store.Store) {
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, as := askRig(t, st, newToken(t, st), newTestClock(), time.Hour)
	ref := r.raiseAsk()
	for _, c := range []struct {
		query    string
		min, max time.Duration
	}{
		{untilQuery("1", time.Now().Add(300*time.Millisecond)), 300 * time.Millisecond, 900 * time.Millisecond},
		{untilQuery("1", time.Now().Add(time.Hour)), time.Second, 1900 * time.Millisecond},
		{untilQuery("5", time.Now().Add(-time.Minute)), 0, 500 * time.Millisecond},
	} {
		started := time.Now()
		w := r.startWait(ref, c.query)
		w.ends(t)
		took := time.Since(started)
		if w.status != http.StatusOK || w.state != "open" || took < c.min || took > c.max {
			t.Errorf("%s: %d %q after %s, want 200 open after %s to %s", c.query, w.status, w.state, took, c.min, c.max)
		}
	}
	if polledAt(t, as, ref) == nil {
		t.Error("the waits did not mark the poll")
	}
}

// A wait with until answers with the ask as read when until passes, whatever
// happens to it after: an end whose transaction is still open at until is not
// in the answer, though it commits before the server answers (on SQLite,
// marking the poll waits for the writer it holds); an end committed before
// until is.
func TestAWaitWithUntilAnswersWithTheAskAsReadAtUntil(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		for _, held := range []bool{true, false} {
			testAWaitWithUntilAnswersAtUntil(t, openStore(t, dbs.New(t)), held)
		}
	})
}

func testAWaitWithUntilAnswersAtUntil(t *testing.T, st *store.Store, held bool) {
	{
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		r, _ := askRig(t, st, newToken(t, st), newTestClock(), time.Hour)
		ref := r.raiseAsk()
		until := time.Now().Add(500 * time.Millisecond)
		release := make(chan struct{})
		var once sync.Once
		ender := &asks.Store{Store: st, Now: func() time.Time {
			if held {
				once.Do(func() { <-release })
			}
			return until.Add(-400 * time.Millisecond)
		}}
		w := r.startWait(ref, untilQuery("5", until))
		ended := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, err := ender.Answer(context.Background(), audit.Actor{Handle: "nik@host", Operator: true}, "key:x", ref, asks.Reply{Answer: "late"})
			ended <- err
		}()
		go func() {
			time.Sleep(time.Until(until.Add(200 * time.Millisecond)))
			close(release)
		}()
		w.ends(t)
		if err := <-ended; err != nil {
			t.Fatal(err)
		}
		want := "answered"
		if held {
			want = "open"
		}
		if w.status != http.StatusOK || w.state != want {
			t.Errorf("held=%v: the wait ended with %d %q, want 200 %q", held, w.status, w.state, want)
		}
	}
}

// A wait that starts while an end already holds the writer (so marking the poll
// at its start waits for it) and whose end commits after until answers with the
// ask still open: until is timed from the wait's start, not from when that
// first write comes back. Answers and withdrawals; and the same when the end is
// held across a renewal of the poll instead (a touch interval shorter than the
// wait). An end committed before until, with the wait started after it, is
// still the answer.
func TestAWaitWithUntilIgnoresAnEndHoldingTheWriterBeforeTheWaitStarted(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		for _, withdraw := range []bool{false, true} {
			for _, touch := range []time.Duration{time.Hour, 50 * time.Millisecond} {
				testAWaitUntilWithEndHoldingTheWriter(t, openStore(t, dbs.New(t)), withdraw, touch)
			}
		}
	})
}

func testAWaitUntilWithEndHoldingTheWriter(t *testing.T, st *store.Store, withdraw bool, touch time.Duration) {
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, _ := askRig(t, st, newToken(t, st), newTestClock(), touch)
	ref := r.raiseAsk()
	start := time.Now()
	until := start.Add(600 * time.Millisecond)
	release := make(chan struct{})
	var once sync.Once
	ender := &asks.Store{Store: st, Now: func() time.Time {
		once.Do(func() { <-release })
		return start
	}}
	ended := make(chan error, 1)
	go func() {
		if withdraw {
			_, err := ender.Withdraw(context.Background(), audit.Actor{Handle: "claude@host"}, "key:x", ref, "moved on")
			ended <- err
			return
		}
		_, err := ender.Answer(context.Background(), audit.Actor{Handle: "nik@host", Operator: true}, "key:x", ref, asks.Reply{Answer: "late"})
		ended <- err
	}()
	time.Sleep(100 * time.Millisecond) // the end holds the writer now
	go func() {
		time.Sleep(time.Until(until.Add(300 * time.Millisecond)))
		close(release)
	}()
	w := r.startWait(ref, untilQuery("5", until))
	w.ends(t)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	if w.status != http.StatusOK || w.state != "open" {
		t.Errorf("withdraw=%v touch=%s: the wait ended with %d %q, want 200 open", withdraw, touch, w.status, w.state)
	}
	if k, err := (&asks.Store{Store: st}).Get(context.Background(), ref); err != nil || k.State == "open" {
		t.Errorf("withdraw=%v touch=%s: the end did not commit (%v)", withdraw, touch, err)
	}
	// started after the end committed, with until still ahead: the end counts
	w = r.startWait(ref, untilQuery("5", time.Now().Add(time.Minute)))
	w.ends(t)
	if w.state == "open" {
		t.Errorf("withdraw=%v touch=%s: a wait started after the end's commit still saw the ask open", withdraw, touch)
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
		if done, err := w.step(nil, nil, nil); done || err != nil {
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
	got := o.read(context.Background(), asks.Ask{State: "open"}, time.Now(), &wait, nil)
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("a wait without a feed = %#v, want a 503 problem", got)
	}
	got = o.read(context.Background(), asks.Ask{State: "answered"}, time.Now(), &wait, nil)
	if _, ok := got.(GetAsk200JSONResponse); !ok {
		t.Errorf("a wait on an answered ask = %#v, want the ask at once", got)
	}
	got = o.read(context.Background(), asks.Ask{State: "open"}, time.Now(), nil, nil)
	if _, ok := got.(GetAsk200JSONResponse); !ok {
		t.Errorf("an open ask without wait = %#v, want the ask at once", got)
	}
}

// until is bound to the instant it names to the nanosecond, sub-millisecond
// part included, the way the generated handler binds it: the read at until is
// timed at the client's deadline itself, never at a rounded one.
func TestUntilKeepsItsSubMillisecondPart(t *testing.T) {
	for in, want := range map[string]time.Time{
		"2026-11-10T12:00:01.000123Z":       time.Date(2026, 11, 10, 12, 0, 1, 123_000, time.UTC),
		"2026-11-10T12:00:01.000000001Z":    time.Date(2026, 11, 10, 12, 0, 1, 1, time.UTC),
		"2026-11-10T14:00:01.9999999+02:00": time.Date(2026, 11, 10, 12, 0, 1, 999_999_900, time.UTC),
	} {
		var until *time.Time
		err := runtime.BindQueryParameterWithOptions("form", true, false, "until", url.Values{"until": {in}}, &until,
			runtime.BindQueryParameterOptions{Type: "string", Format: "date-time"})
		if err != nil || until == nil || !until.Equal(want) {
			t.Errorf("until=%s bound to %v (%v), want %s", in, until, err, want.Format(time.RFC3339Nano))
		}
	}
}
