package feed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/events"
	"github.com/n-orlov/board/internal/store"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// fakeSource is an events table the test fills by hand, with gaps if it likes.
type fakeSource struct {
	mu    sync.Mutex
	items []events.Event
	err   error
}

func (s *fakeSource) put(ns ...int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range ns {
		s.items = append(s.items, events.Event{N: n, ID: fmt.Sprintf("id%d", n)})
	}
}

func (s *fakeSource) After(_ context.Context, n int64, limit int) ([]events.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	var out []events.Event
	for _, e := range s.items {
		if e.N > n && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *fakeSource) Last(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last int64
	for _, e := range s.items {
		last = max(last, e.N)
	}
	return last, s.err
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func drain(s *Subscription) []int64 {
	var got []int64
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return got
			}
			got = append(got, e.N)
		default:
			return got
		}
	}
}

func equal(a, b []int64) bool { return slices.Equal(a, b) }

func TestParseInterval(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", time.Second, false}, {"  ", time.Second, false}, {"250ms", 250 * time.Millisecond, false}, {"3s", 3 * time.Second, false},
		{"soon", 0, true}, {"0", 0, true}, {"0s", 0, true}, {"-1s", 0, true}, {"off", 0, true}, {"5", 0, true},
	} {
		got, err := ParseInterval(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseInterval(%q) = %v, %v; want %v, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "BOARD_STREAM_POLL") {
			t.Errorf("error %q does not name BOARD_STREAM_POLL", err)
		}
	}
}

func TestPollDeliversInOrderOnceToEverySubscriber(t *testing.T) {
	src := &fakeSource{}
	f := NewAt(src, 0, Options{})
	a, b := f.Subscribe(), f.Subscribe()
	src.put(1, 2, 3)
	f.Poll(context.Background())
	f.Poll(context.Background())
	src.put(4)
	f.Poll(context.Background())
	for name, s := range map[string]*Subscription{"a": a, "b": b} {
		if got := drain(s); !equal(got, []int64{1, 2, 3, 4}) {
			t.Errorf("subscriber %s got %v", name, got)
		}
	}
}

func TestSubscriptionAfterMarksWhereTheFeedWas(t *testing.T) {
	src := &fakeSource{}
	src.put(1, 2, 3)
	f := NewAt(src, 2, Options{})
	s := f.Subscribe()
	if s.After != 2 {
		t.Fatalf("After = %d, want 2", s.After)
	}
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{3}) {
		t.Errorf("got %v, want only the event after 2", got)
	}
	if late := f.Subscribe(); late.After != 3 {
		t.Errorf("a later subscriber starts at %d, want 3", late.After)
	}
}

func TestNewStartsAtTheNewestEventAndReportsAnUnreadableTable(t *testing.T) {
	src := &fakeSource{}
	src.put(1, 2, 3, 4)
	f, err := New(context.Background(), src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := f.Subscribe()
	src.put(5)
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{5}) {
		t.Errorf("got %v, want only 5", got)
	}
	src.err = errors.New("db down")
	if _, err := New(context.Background(), src, Options{}); err == nil {
		t.Error("New on an unreadable table did not fail")
	}
}

func TestAGapHoldsLaterEventsUntilTheMissingOneCommits(t *testing.T) {
	src := &fakeSource{}
	c := &clock{now: t0}
	f := NewAt(src, 0, Options{GapGrace: 5 * time.Second, Now: c.Now})
	s := f.Subscribe()
	src.put(1, 3, 4)
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{1}) {
		t.Fatalf("before the gap closes got %v, want [1]", got)
	}
	c.now = t0.Add(4 * time.Second)
	f.Poll(context.Background())
	if got := drain(s); len(got) != 0 {
		t.Fatalf("inside the grace period got %v", got)
	}
	src.mu.Lock()
	src.items = append([]events.Event{src.items[0], {N: 2, ID: "id2"}}, src.items[1:]...)
	src.mu.Unlock()
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{2, 3, 4}) {
		t.Errorf("after the gap closed got %v, want [2 3 4]", got)
	}
}

func TestAGapOlderThanTheGraceIsARolledBackWrite(t *testing.T) {
	src := &fakeSource{}
	c := &clock{now: t0}
	f := NewAt(src, 0, Options{GapGrace: 5 * time.Second, Now: c.Now})
	s := f.Subscribe()
	src.put(1, 3, 4, 6)
	f.Poll(context.Background())
	c.now = t0.Add(5 * time.Second)
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{1, 3, 4}) {
		t.Fatalf("got %v, want [1 3 4]", got)
	}
	// 5 is a new gap, with its own grace period.
	f.Poll(context.Background())
	if got := drain(s); len(got) != 0 {
		t.Fatalf("a second gap was skipped at once: %v", got)
	}
	c.now = t0.Add(10 * time.Second)
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{6}) {
		t.Errorf("got %v, want [6]", got)
	}
}

func TestAFullBatchIsFollowedByTheNext(t *testing.T) {
	src := &fakeSource{}
	var all []int64
	for n := int64(1); n <= 3*batchSize+7; n++ {
		all = append(all, n)
	}
	src.put(all...)
	f := NewAt(src, 0, Options{Buffer: len(all)})
	s := f.Subscribe()
	f.Poll(context.Background())
	if got := drain(s); !equal(got, all) {
		t.Errorf("delivered %d events, want %d", len(got), len(all))
	}
}

func TestASlowSubscriberIsEndedAndTheOthersAreNot(t *testing.T) {
	src := &fakeSource{}
	f := NewAt(src, 0, Options{Buffer: 2})
	slow := f.Subscribe()
	src.put(1, 2)
	f.Poll(context.Background())
	fast := f.Subscribe()
	src.put(3, 4)
	f.Poll(context.Background())
	if got := drain(slow); !equal(got, []int64{1, 2}) {
		t.Errorf("slow got %v", got)
	}
	if err := slow.Err(); !errors.Is(err, ErrOverflow) {
		t.Errorf("slow.Err() = %v, want ErrOverflow", err)
	}
	if got := drain(fast); !equal(got, []int64{3, 4}) {
		t.Errorf("fast got %v", got)
	}
	if err := fast.Err(); err != nil {
		t.Errorf("fast.Err() = %v while it is live", err)
	}
}

func TestCloseEndsASubscriptionOnceAndNothingMoreArrives(t *testing.T) {
	src := &fakeSource{}
	f := NewAt(src, 0, Options{})
	s := f.Subscribe()
	s.Close()
	s.Close()
	if _, ok := <-s.Events(); ok {
		t.Error("the channel is open after Close")
	}
	if err := s.Err(); err != nil {
		t.Errorf("Err after Close = %v", err)
	}
	src.put(1)
	f.Poll(context.Background())
	if len(f.subs) != 0 {
		t.Errorf("%d subscriptions left", len(f.subs))
	}
}

func TestAFailedPollIsReportedAndTheNextOneRecovers(t *testing.T) {
	src := &fakeSource{err: errors.New("db down")}
	var reported []error
	f := NewAt(src, 0, Options{Report: func(err error) { reported = append(reported, err) }})
	s := f.Subscribe()
	f.Poll(context.Background())
	if len(reported) != 1 {
		t.Fatalf("reported %v", reported)
	}
	src.err = nil
	src.put(1)
	f.Poll(context.Background())
	if got := drain(s); !equal(got, []int64{1}) {
		t.Errorf("got %v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src.err = errors.New("canceled")
	f.Poll(ctx)
	if len(reported) != 1 {
		t.Errorf("an error after the context ended was reported: %v", reported)
	}
}

func TestRunEndsEverySubscriptionWhenItsContextEnds(t *testing.T) {
	src := &fakeSource{}
	f := NewAt(src, 0, Options{})
	s := f.Subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if _, ok := <-s.Events(); ok {
		t.Error("the channel is open after Run ended")
	}
	if err := s.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("Err = %v, want context.Canceled", err)
	}
}

// ---- against a real database ----

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

// appendEvent commits one event in its own transaction.
func appendEvent(t *testing.T, st *store.Store, summary string) {
	t.Helper()
	tx, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	insertEvent(t, tx, st, summary)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// insertEvent appends one event in tx, as the actor "tester".
func insertEvent(t *testing.T, tx *sql.Tx, st *store.Store, summary string) {
	t.Helper()
	ctx := context.Background()
	id, err := audit.UpsertActor(ctx, tx, st.Dialect(), audit.Actor{Handle: "tester"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.AppendAs(ctx, tx, st.Dialect(), id, t0, audit.Event{Principal: "tester", Type: "note_posted", Summary: summary}); err != nil {
		t.Fatal(err)
	}
}

func receive(t *testing.T, s *Subscription) events.Event {
	t.Helper()
	const within = 5 * time.Second
	select {
	case e, ok := <-s.Events():
		if !ok {
			t.Fatalf("subscription closed: %v", s.Err())
		}
		return e
	case <-time.After(within):
		t.Fatalf("no event within %v", within)
		return events.Event{}
	}
}

func TestStoreSourceReadsTheTableInOrderOfN(t *testing.T) {
	st := newStore(t)
	src := StoreSource{Store: st}
	if n, err := src.Last(context.Background()); err != nil || n != 0 {
		t.Fatalf("Last on an empty table = %d, %v", n, err)
	}
	for _, s := range []string{"one", "two", "three"} {
		appendEvent(t, st, s)
	}
	if n, err := src.Last(context.Background()); err != nil || n != 3 {
		t.Fatalf("Last = %d, %v", n, err)
	}
	got, err := src.After(context.Background(), 1, 10)
	if err != nil || len(got) != 2 || got[0].N != 2 || got[0].Summary != "two" || got[1].N != 3 {
		t.Errorf("After(1) = %v, %v", got, err)
	}
	if got, _ := src.After(context.Background(), 0, 2); len(got) != 2 || got[1].N != 2 {
		t.Errorf("After(0) with limit 2 = %v", got)
	}
}

// manualTicks is a poll ticker the test fires by hand.
type manualTicks struct {
	ch chan time.Time
}

func (m *manualTicks) tick(time.Duration) (<-chan time.Time, func()) { return m.ch, func() {} }

// signalled is a Source that says each time a read of the table has finished.
type signalled struct {
	Source
	polled chan struct{}
}

func (s signalled) After(ctx context.Context, n int64, limit int) ([]events.Event, error) {
	got, err := s.Source.After(ctx, n, limit)
	select {
	case s.polled <- struct{}{}:
	default:
	}
	return got, err
}

func TestASubscriberReceivesAWriteAtTheNextPollTick(t *testing.T) {
	st := newStore(t)
	ticks := &manualTicks{ch: make(chan time.Time)}
	src := signalled{Source: StoreSource{Store: st}, polled: make(chan struct{}, 1)}
	f, err := New(context.Background(), src, Options{Poll: time.Hour, Tick: ticks.tick})
	if err != nil {
		t.Fatal(err)
	}
	s := f.Subscribe()
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	<-src.polled // the first read is over: the feed now waits for a tick
	appendEvent(t, st, "remote write")
	select {
	case e := <-s.Events():
		t.Fatalf("delivered %q before any tick or wake", e.Summary)
	case <-time.After(50 * time.Millisecond):
	}
	ticks.ch <- t0 // the one tick that ends the poll interval
	if e := receive(t, s); e.Summary != "remote write" || e.N != 1 {
		t.Errorf("got %+v", e)
	}
}

func TestASubscriberReceivesAWriteWithinOnePollIntervalOnARealTimer(t *testing.T) {
	st := newStore(t)
	const poll = 100 * time.Millisecond
	f, err := New(context.Background(), StoreSource{Store: st}, Options{Poll: poll})
	if err != nil {
		t.Fatal(err)
	}
	s := f.Subscribe()
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	start := time.Now()
	appendEvent(t, st, "tick me")
	receive(t, s)
	// One poll interval, plus the scheduling slack of a loaded test host.
	if took := time.Since(start); took > poll+900*time.Millisecond {
		t.Errorf("delivered after %v, with a poll interval of %v", took, poll)
	}
}

func TestALocalWriteIsDeliveredBeforeThePollIntervalElapses(t *testing.T) {
	st := newStore(t)
	ticks := &manualTicks{ch: make(chan time.Time)} // never fires: the poll interval never elapses
	src := signalled{Source: StoreSource{Store: st}, polled: make(chan struct{}, 1)}
	f, err := New(context.Background(), src, Options{Poll: time.Hour, Tick: ticks.tick})
	if err != nil {
		t.Fatal(err)
	}
	s := f.Subscribe()
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	<-src.polled
	appendEvent(t, st, "local write")
	f.Notify()
	f.Notify() // wakes coalesce and never block
	if e := receive(t, s); e.Summary != "local write" {
		t.Errorf("got %+v", e)
	}
}

func goroutines(t *testing.T, atMost int) int {
	t.Helper()
	n := runtime.NumGoroutine()
	for i := 0; i < 100 && n > atMost; i++ {
		time.Sleep(20 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

func TestAnUnsubscribedSubscriberLeaksNoGoroutine(t *testing.T) {
	st := newStore(t)
	f, err := New(context.Background(), StoreSource{Store: st}, Options{Poll: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	appendEvent(t, st, "warm up")
	warm := f.Subscribe()
	receive(t, warm)
	warm.Close()

	base := goroutines(t, 0)
	subs := make([]*Subscription, 50)
	for i := range subs {
		subs[i] = f.Subscribe()
	}
	appendEvent(t, st, "to all")
	f.Notify()
	for _, s := range subs {
		receive(t, s)
	}
	if n := runtime.NumGoroutine(); n > base+2 {
		t.Errorf("50 live subscribers run %d goroutines against a base of %d: a subscriber costs a goroutine", n, base)
	}
	for _, s := range subs {
		s.Close()
	}
	if n := goroutines(t, base); n > base {
		t.Errorf("%d goroutines after unsubscribing, %d before subscribing", n, base)
	}
}
