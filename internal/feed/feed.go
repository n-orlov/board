// Package feed is the change feed of board serve: it tails the events table in
// the order of `n` (SPEC §7) and hands every event to its subscribers once, in
// order. The SSE stream and the ask long-poll are built on it, so that several
// servers on one database each see every event, whichever server took the write.
//
// The feed polls every BOARD_STREAM_POLL and a local write wakes it at once
// through Notify. On Postgres a lower `n` can commit after a higher one, so a
// gap in `n` is held back until the missing event commits or the gap is old
// enough to be a rolled-back write.
package feed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/n-orlov/board/internal/events"
	"github.com/n-orlov/board/internal/store"
)

// DefaultPoll is the poll interval when BOARD_STREAM_POLL is unset.
const DefaultPoll = time.Second

// DefaultGapGrace is how long an event is held behind a missing `n` before the
// feed decides the missing write rolled back and moves on.
const DefaultGapGrace = 2 * time.Second

const (
	defaultBuffer = 256
	batchSize     = 500
)

// ErrOverflow is the reason a subscription ends when its subscriber did not
// keep up. The subscriber has missed events and must resynchronise.
var ErrOverflow = errors.New("feed: subscriber too slow")

// ParseInterval reads BOARD_STREAM_POLL. An empty value is DefaultPoll and a Go
// duration above zero is that interval. Anything else is an error: a poll
// interval of zero would spin.
func ParseInterval(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultPoll, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("BOARD_STREAM_POLL=%q: want a duration above zero such as 1s", v)
	}
	return d, nil
}

// Source reads the events table.
type Source interface {
	// After returns up to limit events with n above n, in order of n.
	After(ctx context.Context, n int64, limit int) ([]events.Event, error)
	// Last returns the highest n in the table, 0 when it is empty.
	Last(ctx context.Context) (int64, error)
}

// StoreSource reads the events of one database.
type StoreSource struct{ Store *store.Store }

// After implements Source.
func (s StoreSource) After(ctx context.Context, n int64, limit int) ([]events.Event, error) {
	q := events.Query{Limit: limit}
	if n > 0 {
		q.Since = fmt.Sprintf("E%d", n)
	}
	p, err := events.Store{Store: s.Store}.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return p.Items, nil
}

// Last implements Source.
func (s StoreSource) Last(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	if err := s.Store.Reader().QueryRowContext(ctx, `SELECT MAX(n) FROM events`).Scan(&n); err != nil {
		return 0, fmt.Errorf("read last event: %w", err)
	}
	return n.Int64, nil
}

// Options tunes a Feed. The zero value is the production setting.
type Options struct {
	// Poll is the poll interval; 0 means DefaultPoll.
	Poll time.Duration
	// GapGrace is how long a gap in `n` is held; 0 means DefaultGapGrace.
	GapGrace time.Duration
	// Buffer is the size of each subscription's channel; 0 means 256.
	Buffer int
	// Now is the clock the gap grace reads; nil means time.Now.
	Now func() time.Time
	// Tick makes the poll ticker; nil means a time.Ticker. Tests inject it.
	Tick func(d time.Duration) (ticks <-chan time.Time, stop func())
	// Report receives the errors of a failed poll; the feed retries on the next
	// tick whatever it does.
	Report func(error)
}

// Feed tails the events table and fans events out to subscribers.
type Feed struct {
	src  Source
	opts Options
	wake chan struct{}

	mu      sync.Mutex
	last    int64
	subs    map[*Subscription]struct{}
	gap     int64
	gapFrom time.Time
}

// New returns a feed positioned at the newest event: it delivers the events
// written from now on. Run starts it.
func New(ctx context.Context, src Source, opts Options) (*Feed, error) {
	last, err := src.Last(ctx)
	if err != nil {
		return nil, err
	}
	return NewAt(src, last, opts), nil
}

// NewAt returns a feed that delivers the events after n.
func NewAt(src Source, n int64, opts Options) *Feed {
	if opts.Poll <= 0 {
		opts.Poll = DefaultPoll
	}
	if opts.GapGrace <= 0 {
		opts.GapGrace = DefaultGapGrace
	}
	if opts.Buffer <= 0 {
		opts.Buffer = defaultBuffer
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Tick == nil {
		opts.Tick = func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		}
	}
	return &Feed{src: src, opts: opts, wake: make(chan struct{}, 1), last: n, subs: map[*Subscription]struct{}{}}
}

// Notify wakes the feed at once; a write calls it after it commits. It never
// blocks and wakes are coalesced.
func (f *Feed) Notify() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// Run tails the table until ctx ends, then ends every subscription.
func (f *Feed) Run(ctx context.Context) {
	defer f.closeAll(context.Canceled)
	ticks, stop := f.opts.Tick(f.opts.Poll)
	defer stop()
	for {
		f.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		case <-f.wake:
		}
	}
}

// Poll reads and delivers every event after the last delivered one.
func (f *Feed) Poll(ctx context.Context) {
	for {
		full, err := f.pollOnce(ctx)
		if err != nil {
			if ctx.Err() == nil && f.opts.Report != nil {
				f.opts.Report(err)
			}
			return
		}
		if !full {
			return
		}
	}
}

// pollOnce reads one batch and delivers it. It reports whether the batch was
// full, so more may be waiting.
func (f *Feed) pollOnce(ctx context.Context) (bool, error) {
	f.mu.Lock()
	from := f.last
	f.mu.Unlock()
	items, err := f.src.After(ctx, from, batchSize)
	if err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range items {
		if !f.admit(e.N) {
			break
		}
		f.deliver(e)
	}
	return len(items) == batchSize && f.last > from, nil
}

// admit decides whether the event with number n is next in line, and moves the
// position to it. A number that skips ahead is held (false) until the gap closes
// or has stayed open for the grace period, which means the missing write rolled
// back; after that the feed moves on.
func (f *Feed) admit(n int64) bool {
	if n <= f.last {
		return false
	}
	if n == f.last+1 {
		f.last = n
		f.gap = 0
		return true
	}
	now := f.opts.Now()
	if f.gap != f.last+1 {
		f.gap, f.gapFrom = f.last+1, now
		return false
	}
	if now.Sub(f.gapFrom) < f.opts.GapGrace {
		return false
	}
	f.last, f.gap = n, 0
	return true
}

// deliver sends e to every subscriber, ending those whose buffer is full.
func (f *Feed) deliver(e events.Event) {
	for s := range f.subs {
		select {
		case s.ch <- e:
		default:
			f.end(s, ErrOverflow)
		}
	}
}

// end removes s and closes its channel. The caller holds f.mu.
func (f *Feed) end(s *Subscription, err error) {
	if _, ok := f.subs[s]; !ok {
		return
	}
	delete(f.subs, s)
	s.err = err
	close(s.ch)
}

func (f *Feed) closeAll(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.subs {
		f.end(s, err)
	}
}

// Subscription is one subscriber's view of the feed.
type Subscription struct {
	f  *Feed
	ch chan events.Event
	// After is the number of the last event the feed had delivered when the
	// subscription began: every event it carries has a higher n.
	After int64
	err   error
}

// Subscribe starts a subscription. The events it carries are exactly those with
// n above Subscription.After, so a subscriber that replays from the table up to
// After and then reads the channel misses nothing and sees nothing twice. It
// costs no goroutine. Close it when done.
func (f *Feed) Subscribe() *Subscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := &Subscription{f: f, ch: make(chan events.Event, f.opts.Buffer), After: f.last}
	f.subs[s] = struct{}{}
	return s
}

// Events is the channel of events, in order. It is closed when the subscription
// ends: Err then says why.
func (s *Subscription) Events() <-chan events.Event { return s.ch }

// Err is why the channel closed: nil after Close, ErrOverflow when the
// subscriber was too slow and context.Canceled when the feed stopped. Read it
// only after the channel has closed.
func (s *Subscription) Err() error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	return s.err
}

// Close ends the subscription. It is safe to call more than once.
func (s *Subscription) Close() {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.end(s, nil)
}
