package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/n-orlov/board/internal/attention"
)

// AttentionRecheck is the longest a stream with the attention topic goes
// without recomputing the counts: overdue watches and expiring leases change
// with time, not with an event (SPEC 7.1).
const AttentionRecheck = 30 * time.Second

// recheckEvery is the interval of the time-driven recompute: AttentionRecheck,
// or the heartbeat when that is shorter, so a test that shortens the heartbeat
// shortens the recheck with it.
func recheckEvery(heartbeat time.Duration) time.Duration {
	return min(heartbeat, AttentionRecheck)
}

// countsWatch is one stream's view of the attention counts: it recomputes them
// and tells whether they differ from the ones the stream sent last.
type countsWatch struct {
	store *attention.Store
	last  *attention.Counts
}

// poll recomputes the counts (for the whole board, no filter). changed is true
// the first time and whenever they differ from the previous call's.
func (w *countsWatch) poll(ctx context.Context) (counts attention.Counts, changed bool, err error) {
	counts, err = w.store.Counts(ctx, attention.Query{})
	if err != nil {
		return counts, false, err
	}
	changed = w.last == nil || *w.last != counts
	w.last = &counts
	return counts, changed, nil
}

// attentionWatch is the watch of a stream that asked for the attention topic,
// nil when it did not or the server has no attention store.
func (r streamResponse) attentionWatch() *countsWatch {
	if !r.topics.attention || r.hub.attention == nil {
		return nil
	}
	return &countsWatch{store: r.hub.attention}
}

// attentionMessage sends `event: attention` with the counts as one JSON line.
func (s *streamWriter) attention(c attention.Counts) bool {
	data, err := json.Marshal(apiCounts(c))
	if err != nil {
		return false
	}
	return s.write("event: attention\ndata: " + string(data) + "\n\n")
}

// sendCounts recomputes the counts and sends them when they changed. A failed
// read is not the end of the stream: the next event or recheck tries again.
func (s *liveStream) sendCounts() bool {
	if s.watch == nil {
		return true
	}
	counts, changed, err := s.watch.poll(s.r.ctx)
	return err != nil || !changed || s.out.attention(counts)
}
