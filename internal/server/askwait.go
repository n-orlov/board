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

// askWaitProblem is the 400 for a wait outside 1 to 300, nil when it is fine.
func askWaitProblem(wait *int) *problemResponse {
	if wait == nil || (*wait >= minAskWait && *wait <= maxAskWait) {
		return nil
	}
	p := failure(http.StatusBadRequest, "wait must be from 1 to 300 seconds").withNext(nextAskWait)
	return &p
}

// askWaitResponse is the response of an ask read with ?wait=N while the ask is
// open: writing it is the wait.
type askWaitResponse struct {
	ctx context.Context
	ops askOps
	ask asks.Ask
	max time.Duration
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

// hold waits and returns the ask after it, with poll_seen_at set to the end of
// the wait.
func (r askWaitResponse) hold() (asks.Ask, error) {
	w := &waiter{ctx: r.ctx, store: r.ops.asks, hub: r.ops.waits, k: r.ask, sub: r.ops.waits.feed.Subscribe()}
	defer func() { w.sub.Close() }()
	if err := w.begin(); err != nil || w.k.State != domain.AskOpen {
		return w.k, err
	}
	timeout, beat := time.NewTimer(r.max), time.NewTicker(r.touchEvery())
	defer timeout.Stop()
	defer beat.Stop()
	for w.k.State == domain.AskOpen {
		done, err := w.step(timeout.C, beat.C)
		if err != nil {
			return w.k, err
		}
		if done {
			break
		}
	}
	return w.end()
}

// waiter is one held wait: the ask as last read and its place in the change feed.
type waiter struct {
	ctx   context.Context
	store *asks.Store
	hub   *streamHub
	k     asks.Ask
	sub   *feed.Subscription
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
		w.k = k
	}
	return err
}

// step blocks until something happens and handles it: done is true when the
// wait is over whatever the ask's state.
func (w *waiter) step(timeout, beat <-chan time.Time) (done bool, err error) {
	select {
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
	return w.finish(context.WithoutCancel(w.ctx))
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
