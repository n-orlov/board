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
	return streamResponse{ctx: ctx, hub: o.hub, topics: topics}, nil
}

// streamResponse is the response of an open stream; writing it is the stream.
type streamResponse struct {
	ctx    context.Context
	hub    *streamHub
	topics streamTopics
}

// VisitGetStreamResponse sends the headers, then one message per event, flushed
// as written, and a comment heartbeat while idle. It returns when the client
// disconnects, the server shuts down or the feed ends the subscription.
func (r streamResponse) VisitGetStreamResponse(w http.ResponseWriter) error {
	events, unsubscribe := r.subscribe()
	defer unsubscribe()
	out := &streamWriter{w: w, rc: http.NewResponseController(w)}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !out.flush() {
		return nil
	}
	beat := time.NewTicker(r.hub.heartbeat)
	defer beat.Stop()
	for r.step(out, events, beat.C) {
	}
	return nil
}

// subscribe joins the change feed when the stream asked for events; the channel
// is nil otherwise, which blocks forever in a select.
func (r streamResponse) subscribe() (<-chan events.Event, func()) {
	if !r.topics.events {
		return nil, func() {}
	}
	sub := r.hub.feed.Subscribe()
	return sub.Events(), sub.Close
}

// step waits for the next thing to send and sends it. It reports false when the
// stream is over: the client left, the server is shutting down, the feed ended
// the subscription or a write failed.
func (r streamResponse) step(out *streamWriter, events <-chan events.Event, beat <-chan time.Time) bool {
	select {
	case <-r.ctx.Done():
		return false
	case <-r.hub.stop:
		return false
	case e, ok := <-events:
		return ok && out.message(e)
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
