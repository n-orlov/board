package timeline

import (
	"time"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/store"
)

// activeSpan is how far back an event makes a task active, for the window the
// query names (8h, 24h by default, 3d). It is InvalidError for any other text,
// whether or not the query asks for active tasks: a window that does not read
// is refused as every other parameter is.
func activeSpan(q Query) (time.Duration, error) {
	window := q.Window
	if window == "" {
		window = attention.DefaultWindow
	}
	span, ok := attention.WindowSpan(window)
	if !ok {
		return 0, InvalidError{Msg: "window " + window + " is not 8h, 24h or 3d", Next: attention.NextWindow}
	}
	return span, nil
}

// activeFilter keeps only the active tasks when q asks for them: a task with a
// live lease, or one of q.Attention (the refs of the tasks that carry any
// attention reason), or with an event not older than the window.
func (f *filter) activeFilter(q Query, now time.Time, span time.Duration) {
	if !q.Active {
		return
	}
	cond := `EXISTS (SELECT 1 FROM leases al WHERE al.task_id = t.id AND al.state = 'live' AND al.expires_at >= ?)` +
		` OR EXISTS (SELECT 1 FROM events ae WHERE ae.task_id = t.id AND ae.at >= ?)`
	args := []any{store.EncodeBound(now), store.EncodeBound(now.Add(-span))}
	if len(q.Attention) > 0 {
		cond += ` OR (i.slug || '-' || CAST(t.seq AS TEXT)) IN (` + marks(len(q.Attention)) + `)`
		for _, ref := range q.Attention {
			args = append(args, ref)
		}
	}
	f.add(`(`+cond+`)`, args...)
}
