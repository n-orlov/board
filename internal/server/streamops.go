package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/n-orlov/board/internal/events"
	"github.com/n-orlov/board/internal/feed"
)

// DefaultHeartbeat is the interval between `: ping` comments when
// BOARD_STREAM_HEARTBEAT is unset.
const DefaultHeartbeat = 25 * time.Second

// streamWriteWindow is how long one write of the stream may take: the write
// deadline is moved this far ahead before every write, so a stream outlives
// the server's write timeout while a client that stopped reading still ends it.
const streamWriteWindow = 10 * time.Second

// The reasons of a `reset` message.
const (
	resetUnknownID = "unknown_id"
	resetTooMany   = "too_many"
)

// ParseHeartbeat reads BOARD_STREAM_HEARTBEAT. An empty value is
// DefaultHeartbeat and a Go duration above zero is that interval. Anything
// else is an error: a heartbeat of zero would spin.
func ParseHeartbeat(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultHeartbeat, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("BOARD_STREAM_HEARTBEAT=%q: want a duration above zero such as 25s", v)
	}
	return d, nil
}

// streamHub is what the streams of one server share: the change feed they
// subscribe to, the heartbeat interval, and the signal that ends them all when
// the server shuts down (http.Server.Shutdown does not end a handler).
type streamHub struct {
	feed      *feed.Feed
	events    *events.Store // where a resuming stream reads what it missed; nil: every id is unknown
	heartbeat time.Duration
	stop      chan struct{}
	once      sync.Once
}

// Close ends every stream, now and any that starts later. It is safe to call
// more than once.
func (h *streamHub) Close() { h.once.Do(func() { close(h.stop) }) }

// streamOps is GET /api/v1/stream (SPEC 7.1).
type streamOps struct {
	hub *streamHub
}

func (p problemResponse) VisitGetStreamResponse(w http.ResponseWriter) error { return p.write(w) }

// streamTopics is the set of topics a stream was asked for.
type streamTopics struct{ events, attention bool }

// parseTopics reads the topics parameter: both topics when it is absent, a
// problem naming the first topic that is neither events nor attention.
func parseTopics(param *[]GetStreamParamsTopics) (streamTopics, *problemResponse) {
	if param == nil {
		return streamTopics{events: true, attention: true}, nil
	}
	var t streamTopics
	for _, topic := range *param {
		switch topic {
		case Events:
			t.events = true
		case Attention:
			t.attention = true
		default:
			p := failure(http.StatusBadRequest, fmt.Sprintf("topic %q is not events or attention", string(topic))).
				withNext("Send topics=events, topics=attention or topics=events,attention, or leave topics out for both.")
			return streamTopics{}, &p
		}
	}
	return t, nil
}

// GetStream answers with a stream that lives until the client leaves or the
// server shuts down.
func (o streamOps) GetStream(ctx context.Context, req GetStreamRequestObject) (GetStreamResponseObject, error) {
	if o.hub == nil {
		return failure(http.StatusServiceUnavailable, "the stream is not available"), nil
	}
	topics, problem := parseTopics(req.Params.Topics)
	if problem != nil {
		return *problem, nil
	}
	return streamResponse{ctx: ctx, hub: o.hub, topics: topics, lastID: lastEventID(req.Params)}, nil
}

// lastEventID is the id a stream resumes after: the Last-Event-ID header, else
// the last_event_id parameter, "" when neither is sent.
func lastEventID(p GetStreamParams) string {
	if p.LastEventID != nil && strings.TrimSpace(*p.LastEventID) != "" {
		return strings.TrimSpace(*p.LastEventID)
	}
	if p.LastEventId != nil {
		return strings.TrimSpace(*p.LastEventId)
	}
	return ""
}

// streamResponse is the response of an open stream; writing it is the stream.
type streamResponse struct {
	ctx    context.Context
	hub    *streamHub
	topics streamTopics
	lastID string
}

// VisitGetStreamResponse sends the headers, then the events a resuming client
// missed, then one message per live event, flushed as written, and a comment
// heartbeat while idle. It returns when the client disconnects, the server shuts
// down or the feed ends the subscription.
func (r streamResponse) VisitGetStreamResponse(w http.ResponseWriter) error {
	sub := r.subscribe()
	defer sub.close()
	missed, err := r.resume(sub.after)
	if err != nil {
		return failure(http.StatusInternalServerError, "could not read the events to resume from").write(w)
	}
	out := &streamWriter{w: w, rc: http.NewResponseController(w)}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !out.flush() || !out.replay(missed) {
		return nil
	}
	beat := time.NewTicker(r.hub.heartbeat)
	defer beat.Stop()
	for r.step(out, sub.events, beat.C, missed.skipThrough) {
	}
	return nil
}

// subscription is a stream's place in the change feed: the channel of live
// events (nil when the stream did not ask for events, which blocks forever in a
// select), the feed position it began at and how to leave.
type subscription struct {
	events <-chan events.Event
	after  int64
	close  func()
}

// subscribe joins the change feed when the stream asked for events.
func (r streamResponse) subscribe() subscription {
	if !r.topics.events {
		return subscription{close: func() {}}
	}
	sub := r.hub.feed.Subscribe()
	return subscription{events: sub.Events(), after: sub.After, close: sub.Close}
}

// missed is what a resuming stream sends before its live part: a reset (with
// its reason) or the events it missed. skipThrough is the number of the last
// event the client already has, below which a live event is a duplicate (the
// client may have read it from a server whose feed is further ahead).
type missed struct {
	reset       string
	items       []events.Event
	skipThrough int64
}

// resume works out what the client owes a reconnect with Last-Event-ID, given
// the feed position its subscription began at.
func (r streamResponse) resume(position int64) (missed, error) {
	if r.lastID == "" || !r.topics.events {
		return missed{}, nil
	}
	return outcome(r.hub.resume(r.ctx, r.lastID, position))
}

// resume reads what a client that last saw id has missed. A hub with no events
// store knows no id.
func (h *streamHub) resume(ctx context.Context, id string, position int64) (events.Resume, error) {
	if h.events == nil {
		return events.Resume{}, nil
	}
	return h.events.Resume(ctx, id, position)
}

// outcome turns what the events store found into what the stream sends.
func outcome(res events.Resume, err error) (missed, error) {
	switch {
	case err != nil:
		return missed{}, err
	case !res.Known:
		return missed{reset: resetUnknownID}, nil
	case res.TooMany:
		return missed{reset: resetTooMany}, nil
	}
	return missed{items: res.Items, skipThrough: res.From}, nil
}

// step waits for the next thing to send and sends it. It reports false when the
// stream is over: the client left, the server is shutting down, the feed ended
// the subscription or a write failed.
func (r streamResponse) step(out *streamWriter, events <-chan events.Event, beat <-chan time.Time, skipThrough int64) bool {
	select {
	case <-r.ctx.Done():
		return false
	case <-r.hub.stop:
		return false
	case e, ok := <-events:
		return ok && (e.N <= skipThrough || out.message(e))
	case <-beat:
		return out.comment("ping")
	}
}

// streamWriter writes the messages of a stream and flushes each one.
type streamWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

// flush pushes what is written to the client. It reports false once the client
// is gone.
func (s *streamWriter) flush() bool {
	// A writer without deadlines (a recorder) reports ErrNotSupported: nothing to extend.
	_ = s.rc.SetWriteDeadline(time.Now().Add(streamWriteWindow))
	return s.rc.Flush() == nil
}

// write sends text and flushes it, reporting false once the client is gone.
func (s *streamWriter) write(text string) bool {
	_ = s.rc.SetWriteDeadline(time.Now().Add(streamWriteWindow))
	if _, err := io.WriteString(s.w, text); err != nil {
		return false
	}
	return s.rc.Flush() == nil
}

// replay sends what a resuming client missed: a reset, or the events in order.
func (s *streamWriter) replay(m missed) bool {
	if m.reset != "" {
		return s.write("event: reset\ndata: {\"reason\":\"" + m.reset + "\"}\n\n")
	}
	for _, e := range m.items {
		if !s.message(e) {
			return false
		}
	}
	return true
}

func (s *streamWriter) comment(text string) bool { return s.write(": " + text + "\n\n") }

// message sends the event as `event: board`, with its ULID as the id and the
// JSON `GET /api/v1/events` lists as the data, on one line.
func (s *streamWriter) message(e events.Event) bool {
	data, err := json.Marshal(apiEvent(e))
	if err != nil {
		return false
	}
	return s.write("event: board\nid: " + e.ID + "\ndata: " + string(data) + "\n\n")
}
