package server

import (
	"context"
	"net/http"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/events"
	"github.com/n-orlov/board/internal/feed"
)

// The seconds an ask long-poll may hold (SPEC 7.2).
const (
	minAskWait = 1
	maxAskWait = 300
)

// DefaultAskTouch is how often a held wait renews the ask's poll_seen_at.
const DefaultAskTouch = 30 * time.Second

const nextAskWait = "Send wait as a whole number of seconds from 1 to 300, or leave it out to read the ask at once."

// askWaitProblem is the 400 for a wait outside 1 to 300 and for an until
// without a wait, nil when both are fine.
func askWaitProblem(wait *int, until *time.Time) *problemResponse {
	var p problemResponse
	switch {
	case wait == nil && until != nil:
		p = failure(http.StatusBadRequest, "until is allowed with wait only").withNext("Send wait with until, or leave until out.")
	case wait == nil || (*wait >= minAskWait && *wait <= maxAskWait):
		return nil
	default:
		p = failure(http.StatusBadRequest, "wait must be from 1 to 300 seconds").withNext(nextAskWait)
	}
	return &p
}

// askWaitResponse is the response of an ask read with ?wait=N while the ask is
// open: writing it is the wait. until, when set, is the instant the hold ends
// at if it comes first (SPEC 7.2).
type askWaitResponse struct {
	ctx   context.Context
	ops   askOps
	ask   asks.Ask
	max   time.Duration
	until *time.Time
}

// VisitGetAskResponse holds the request until the ask is answered or withdrawn,
// the wait runs out or the client leaves, then answers with the ask as it then
// is. The write deadline is moved past the wait first, so a server write
// timeout shorter than the wait does not cut it.
func (r askWaitResponse) VisitGetAskResponse(w http.ResponseWriter) error {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(r.max + streamWriteWindow))
	k, err := r.hold()
	switch {
	case err != nil:
		return failure(http.StatusInternalServerError, "could not read the ask").write(w)
	case r.ctx.Err() != nil:
		return nil // the client left
	}
	return GetAsk200JSONResponse(r.ops.apiAsk(k)).VisitGetAskResponse(w)
}

func (r askWaitResponse) touchEvery() time.Duration {
	if r.ops.touch > 0 {
		return r.ops.touch
	}
	return DefaultAskTouch
}

// limits are what end the hold: the wait running out (timeout), or, when until
// comes before that, the read made as until passes (asOf), and never both.
func (r askWaitResponse) limits(w *waiter) (timeout <-chan time.Time, asOf <-chan snapshot, stop func()) {
	if r.until != nil && time.Until(*r.until) <= r.max {
		return nil, w.readAt(*r.until), func() {}
	}
	t := time.NewTimer(r.max)
	return t.C, nil, func() { t.Stop() }
}

// hold waits and returns the ask after it, with poll_seen_at set to the end of
// the wait. With until, an end that no read made by until found is replaced by
// the read made as until passed: the ask as it stood then.
func (r askWaitResponse) hold() (asks.Ask, error) {
	w := &waiter{ctx: r.ctx, store: r.ops.asks, hub: r.ops.waits, k: r.ask, sub: r.ops.waits.feed.Subscribe()}
	defer func() { w.sub.Close() }()
	if err := w.begin(); err != nil || w.k.State != domain.AskOpen {
		return w.k, err
	}
	timeout, asOf, stop := r.limits(w)
	defer stop()
	beat := time.NewTicker(r.touchEvery())
	defer beat.Stop()
	if err := w.run(timeout, beat.C, asOf); err != nil {
		return w.k, err
	}
	if asOf != nil && w.foundLate(*r.until) {
		w.take(<-asOf)
	}
	return w.end()
}

// run steps the wait until it is over: the ask ended, the read at until came
// back, or step says so.
func (w *waiter) run(timeout, beat <-chan time.Time, asOf <-chan snapshot) error {
	for w.k.State == domain.AskOpen && !w.final {
		done, err := w.step(timeout, beat, asOf)
		if err != nil || done {
			return err
		}
	}
	return nil
}

// foundLate says a read during the wait found the ask ended but came back only
// after until, so it may have found an end that took effect after until.
func (w *waiter) foundLate(until time.Time) bool {
	return !w.final && w.k.State != domain.AskOpen && w.readTime.After(until)
}

// snapshot is the ask as one read found it.
type snapshot struct {
	k   asks.Ask
	err error
}

// waiter is one held wait: the ask as last read and its place in the change feed.
//   - readTime is when the last read of the ask came back;
//   - final is set once k is the ask as read when until passed, or the read
//     that failed then; err is that read's error.
type waiter struct {
	ctx      context.Context
	store    *asks.Store
	hub      *streamHub
	k        asks.Ask
	sub      *feed.Subscription
	readTime time.Time
	final    bool
	err      error
}

// readAt reads the ask once at, on the readers, so a write holding the writer
// does not delay it, and delivers what it found. A client that leaves first
// is delivered as the read's error.
func (w *waiter) readAt(at time.Time) <-chan snapshot {
	out := make(chan snapshot, 1)
	go func() {
		t := time.NewTimer(time.Until(at))
		defer t.Stop()
		select {
		case <-t.C:
		case <-w.ctx.Done():
			out <- snapshot{err: w.ctx.Err()}
			return
		}
		k, err := w.store.Get(context.WithoutCancel(w.ctx), w.k.ID)
		out <- snapshot{k: k, err: err}
	}()
	return out
}

// take makes the read made as until passed the ask the wait ends with, unless
// the client left first: the wait then ends as any wait a client left does.
func (w *waiter) take(s snapshot) {
	if s.err != nil && w.ctx.Err() != nil {
		return
	}
	w.final, w.err = true, s.err
	if s.err == nil {
		w.k = s.k
	}
}

// begin reads the ask again, now that the subscription sees every later change,
// and marks the poll when the ask is still open.
func (w *waiter) begin() error {
	if err := w.reread(w.ctx); err != nil || w.k.State != domain.AskOpen {
		return err
	}
	return w.store.Touch(w.ctx, w.k.ID)
}

func (w *waiter) reread(ctx context.Context) error {
	k, err := w.store.Get(ctx, w.k.ID)
	if err == nil {
		w.k, w.readTime = k, time.Now()
	}
	return err
}

// step blocks until something happens and handles it: done is true when the
// wait is over whatever the ask's state.
func (w *waiter) step(timeout, beat <-chan time.Time, asOf <-chan snapshot) (done bool, err error) {
	select {
	case s := <-asOf:
		w.take(s)
		return true, nil
	case <-w.ctx.Done():
		return true, nil
	case <-w.hub.stop:
		return true, nil
	case <-timeout:
		return true, nil
	case <-beat:
		return false, w.renew()
	case e, ok := <-w.sub.Events():
		return false, w.wake(e, ok)
	}
}

// renew marks the poll again and reads the ask, which also covers a feed that
// missed a change.
func (w *waiter) renew() error {
	if err := w.store.Touch(w.ctx, w.k.ID); err != nil {
		return err
	}
	return w.reread(w.ctx)
}

// wake handles one message of the feed: an event of this ask that moves it out
// of open re-reads it, and a subscription the feed ended (a slow subscriber)
// starts again and re-reads, so no change is lost.
func (w *waiter) wake(e events.Event, open bool) error {
	if !open {
		w.sub = w.hub.feed.Subscribe()
		return w.reread(w.ctx)
	}
	if e.RefID != w.k.ID || (e.Type != asks.EventAnswered && e.Type != asks.EventWithdrawn) {
		return nil
	}
	return w.reread(w.ctx)
}

// end marks the poll at the end of the wait, even when the client has left, and
// returns the ask as it then is.
func (w *waiter) end() (asks.Ask, error) {
	if w.final {
		return w.settle(context.WithoutCancel(w.ctx))
	}
	return w.finish(context.WithoutCancel(w.ctx))
}

// settle ends a wait whose ask is the one read as until passed: it is not read
// again, which would find the ask as it is later. The poll is marked when the
// writer is free within 5 seconds; the ask is the answer either way.
func (w *waiter) settle(ctx context.Context) (asks.Ask, error) {
	if w.err != nil {
		return w.k, w.err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if at, err := w.store.MarkPoll(ctx, w.k.ID); err == nil {
		w.k.PollSeenAt = &at
	}
	return w.k, nil
}

func (w *waiter) finish(ctx context.Context) (asks.Ask, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := w.store.Touch(ctx, w.k.ID); err != nil {
		return w.k, err
	}
	err := w.reread(ctx)
	return w.k, err
}
