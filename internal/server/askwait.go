package server

import (
	"context"
	"net/http"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/events"
	"github.com/n-orlov/village-hall/internal/feed"
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
// is, or, with until, as it stood at until (atUntil). The write deadline is moved past the wait first, so a server write
// timeout shorter than the wait does not cut it.
func (r askWaitResponse) VisitGetAskResponse(w http.ResponseWriter) error {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(r.max + streamWriteWindow))
	k, proven, err := r.hold()
	switch {
	case err != nil:
		return failure(http.StatusInternalServerError, "could not read the ask").write(w)
	case r.ctx.Err() != nil:
		return nil // the client left
	}
	return GetAsk200JSONResponse(r.ops.apiAsk(atUntil(k, r.until, proven))).VisitGetAskResponse(w)
}

// atUntil is k as it stood at until, when there is one. An end is in it only
// when there is proof it committed by until: proven, a read of the ask that came
// back at or before until saw it ended, or its commit mark, read after the end's
// COMMIT returned, is at or before until (asks.Ask.AsOf). Any other end had not
// committed by until as far as the server can tell, however early its stamps
// (answered_at, withdrawn_at, anything read inside its transaction) and however
// late the ask is read, so k is then still open.
func atUntil(k asks.Ask, until *time.Time, proven bool) asks.Ask {
	if until == nil || proven {
		return k
	}
	return k.AsOf(*until)
}

// readProves says a read of the ask that found it in state, and came back at
// readBack, proves its end committed by until.
func readProves(state string, readBack time.Time, until *time.Time) bool {
	return until != nil && state != domain.AskOpen && !readBack.After(*until)
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
// the wait, and whether a read that came back by until found it ended (proven).
// With until, an end that no read made by until found is replaced by the read
// made as until passed: the ask as it stood then. That read is timed before any
// write of the wait is made, so a write held up by the writer, begin's included,
// does not move it.
func (r askWaitResponse) hold() (k asks.Ask, proven bool, err error) {
	w := &waiter{ctx: r.ctx, store: r.ops.asks, hub: r.ops.waits, k: r.ask, sub: r.ops.waits.feed.Subscribe(), until: r.until}
	defer func() { w.sub.Close() }()
	// The limits come first: the read at until is timed from here, not from
	// when begin's write (which waits for the writer) comes back.
	timeout, asOf, stop := r.limits(w)
	defer stop()
	if over, err := r.begin(w, asOf); over || err != nil {
		return w.k, w.proven, err
	}
	beat := time.NewTicker(r.touchEvery())
	defer beat.Stop()
	if err := w.run(timeout, beat.C, asOf); err != nil {
		return w.k, w.proven, err
	}
	if asOf != nil && w.foundLate(*r.until) {
		w.take(<-asOf)
	}
	k, err = w.end()
	return k, w.proven, err
}

// begin starts the wait and says whether it is over already: the ask was found
// ended, and by a read that came back by until, or until's own read is the answer.
func (r askWaitResponse) begin(w *waiter, asOf <-chan snapshot) (over bool, err error) {
	if err := w.begin(); err != nil {
		return false, err
	}
	return w.k.State != domain.AskOpen && (asOf == nil || !w.foundLate(*r.until)), nil
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
//   - until is the deadline of the wait, if any, and proven is set once a read
//     that came back at or before it found the ask ended: that end committed by
//     until, however the ask is read after;
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
	until    *time.Time
	proven   bool
}

// readAt reads the ask once at, on the readers, so a write holding the writer
// does not delay it, and delivers what it found. A client that leaves first
// is delivered as the read's error.
func (w *waiter) readAt(at time.Time) <-chan snapshot {
	out := make(chan snapshot, 1)
	id := w.k.ID // read here: the wait rereads w.k while the goroutine runs
	go func() {
		t := time.NewTimer(time.Until(at))
		defer t.Stop()
		select {
		case <-t.C:
		case <-w.ctx.Done():
			out <- snapshot{err: w.ctx.Err()}
			return
		}
		k, err := w.store.Get(context.WithoutCancel(w.ctx), id)
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
	return w.touch(w.ctx)
}

// touch marks the poll and wakes the change feed, so the ask_polling event a
// mark may append reaches the streams of this server at once.
func (w *waiter) touch(ctx context.Context) error {
	err := w.store.Touch(ctx, w.k.ID)
	w.hub.feed.Notify()
	return err
}

func (w *waiter) reread(ctx context.Context) error {
	k, err := w.store.Get(ctx, w.k.ID)
	if err == nil {
		w.k, w.readTime = k, time.Now()
		w.proven = w.proven || readProves(k.State, w.readTime, w.until)
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
	if err := w.touch(w.ctx); err != nil {
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

// settle ends a wait whose ask is the one read as until passed: the ask is not
// read again for its state, which would find it as it is later. The poll is
// marked when the writer is free within 5 seconds; the ask is the answer
// either way. The write waits for the writer, so a commit mark whose write was
// still in flight when until passed has landed by the time it returns, and the
// mark is then taken in (adoptMark).
func (w *waiter) settle(ctx context.Context) (asks.Ask, error) {
	if w.err != nil {
		return w.k, w.err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if at, err := w.store.MarkPoll(ctx, w.k.ID); err == nil {
		w.k.PollSeenAt = &at
	}
	w.adoptMark(ctx)
	return w.k, nil
}

// adoptMark takes the commit mark of the ask's end from a read made after the
// poll's write, when the snapshot taken as until passed had none, and the mark
// is at or before until. The mark is a real-clock sample read after the end's
// COMMIT returned, so being at or before until proves the end committed by
// then however late it was written or read, and the end cannot change after.
// A mark after until, or none, leaves the snapshot as it is, and the end stays
// uncounted.
func (w *waiter) adoptMark(ctx context.Context) {
	if w.until != nil {
		w.k = adoptedMark(ctx, w.store, w.k, *w.until)
	}
}

// adoptedMark is k with the commit mark of its end taken from a fresh read,
// when k, an ask found ended, has none and the stored mark is at or before
// until; otherwise k itself.
func adoptedMark(ctx context.Context, store *asks.Store, k asks.Ask, until time.Time) asks.Ask {
	if k.EndCommitAt != nil || ctx.Err() != nil {
		return k
	}
	got, err := store.Get(ctx, k.ID)
	if err != nil || got.State == domain.AskOpen || got.EndCommitAt == nil || got.EndCommitAt.After(until) {
		return k
	}
	got.PollSeenAt = k.PollSeenAt
	return got
}

func (w *waiter) finish(ctx context.Context) (asks.Ask, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := w.touch(ctx); err != nil {
		return w.k, err
	}
	err := w.reread(ctx)
	return w.k, err
}
