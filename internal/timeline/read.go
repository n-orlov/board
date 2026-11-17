package timeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

// each runs query with args on the reader and calls scan on every row.
func (s Store) each(ctx context.Context, what, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := s.Store.ReaderFor(ctx).QueryContext(ctx, s.Store.Dialect().Rebind(query), args...)
	if err != nil {
		return fmt.Errorf("read timeline %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("read timeline %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read timeline %s: %w", what, err)
	}
	return nil
}

// instantOf is the time of a nullable stored instant, nil when it is null.
func instantOf(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := store.DecodeTime(v.Int64)
	return &t
}

// tasks reads the tasks the filter keeps, with the refs of the tasks each is
// blocked by.
func (s Store) tasks(ctx context.Context, f *filter) ([]Task, error) {
	out := []Task{}
	byID := map[string]int{}
	err := s.each(ctx, "tasks", `SELECT t.id, i.slug, t.seq, t.title, t.status, t.kind, t.planned_start, t.planned_end, t.created_at, t.closed_at
		FROM tasks t JOIN initiatives i ON i.id = t.initiative_id`+f.where()+` ORDER BY i.n, t.seq`, f.args, func(r *sql.Rows) error {
		var t Task
		var seq int
		var ps, pe, created, closed sql.NullInt64
		if err := r.Scan(&t.ID, &t.Initiative, &seq, &t.Title, &t.Status, &t.Kind, &ps, &pe, &created, &closed); err != nil {
			return err
		}
		t.Ref, t.PlannedStart, t.PlannedEnd, t.CreatedAt, t.ClosedAt = domain.FormatTaskRef(t.Initiative, seq), instantOf(ps), instantOf(pe), store.DecodeTime(created.Int64), instantOf(closed)
		t.Deps = []string{}
		byID[t.ID] = len(out)
		out = append(out, t)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, s.deps(ctx, f, out, byID)
}

// deps fills in the Deps of tasks, in the order of the blocking tasks'
// initiatives and numbers.
func (s Store) deps(ctx context.Context, f *filter, tasks []Task, byID map[string]int) error {
	return s.each(ctx, "deps", `SELECT d.task_id, bi.slug, bt.seq
		FROM task_deps d JOIN tasks t ON t.id = d.task_id JOIN initiatives i ON i.id = t.initiative_id
		JOIN tasks bt ON bt.id = d.blocked_by_task_id JOIN initiatives bi ON bi.id = bt.initiative_id`+f.where()+` ORDER BY bi.n, bt.seq`, f.args, func(r *sql.Rows) error {
		var id, slug string
		var seq int
		if err := r.Scan(&id, &slug, &seq); err != nil {
			return err
		}
		t := &tasks[byID[id]]
		t.Deps = append(t.Deps, domain.FormatTaskRef(slug, seq))
		return nil
	})
}

// initiatives reads the initiatives that hold a task the filter keeps, in
// board order, with their tags.
func (s Store) initiatives(ctx context.Context, f *filter) ([]Initiative, error) {
	out := []Initiative{}
	byID := map[string]int{}
	err := s.each(ctx, "initiatives", `SELECT DISTINCT i.n, i.id, i.slug, i.title, i.status
		FROM tasks t JOIN initiatives i ON i.id = t.initiative_id`+f.where()+` ORDER BY i.n`, f.args, func(r *sql.Rows) error {
		var n int64
		var id, status string
		var ini Initiative
		if err := r.Scan(&n, &id, &ini.Slug, &ini.Title, &status); err != nil {
			return err
		}
		ini.Closed, ini.Tags = status == "closed", []string{}
		byID[id] = len(out)
		out = append(out, ini)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, s.tags(ctx, f, out, byID)
}

func (s Store) tags(ctx context.Context, f *filter, list []Initiative, byID map[string]int) error {
	return s.each(ctx, "tags", `SELECT DISTINCT it.initiative_id, it.tag
		FROM initiative_tags it JOIN tasks t ON t.initiative_id = it.initiative_id JOIN initiatives i ON i.id = t.initiative_id`+f.where()+` ORDER BY it.initiative_id, it.tag`, f.args, func(r *sql.Rows) error {
		var id, tag string
		if err := r.Scan(&id, &tag); err != nil {
			return err
		}
		ini := &list[byID[id]]
		ini.Tags = append(ini.Tags, tag)
		return nil
	})
}

// leases reads the leases of the kept tasks that overlap [from, to], by lease number.
func (s Store) leases(ctx context.Context, f *filter, from, to time.Time) ([]Lease, error) {
	out := []Lease{}
	args := append(append([]any{}, f.args...), store.EncodeTime(to), store.EncodeTime(from))
	err := s.each(ctx, "leases", `SELECT l.n, i.slug, t.seq, a.handle, l.state, l.intent, l.started_at, l.ended_at, l.expires_at
		FROM leases l JOIN tasks t ON t.id = l.task_id JOIN initiatives i ON i.id = t.initiative_id JOIN actors a ON a.id = l.actor_id`+
		f.where()+` AND `+leaseOverlap+` ORDER BY l.n`, args, func(r *sql.Rows) error {
		var l Lease
		var n int64
		var seq int
		var started, expires int64
		var ended sql.NullInt64
		var slug string
		if err := r.Scan(&n, &slug, &seq, &l.Holder, &l.State, &l.Intent, &started, &ended, &expires); err != nil {
			return err
		}
		l.Ref, l.Task, l.StartedAt, l.EndedAt = domain.FormatLeaseRef(n), domain.FormatTaskRef(slug, seq), store.DecodeTime(started), leaseEnd(l.State, ended, expires)
		out = append(out, l)
		return nil
	})
	return out, err
}

// leaseEnd is when a lease ended: nil while it is live, its end for an ended one
// and its expiry for an expired one.
func leaseEnd(state string, ended sql.NullInt64, expires int64) *time.Time {
	switch {
	case state == domain.LeaseLive:
		return nil
	case ended.Valid:
		return instantOf(ended)
	}
	return instantOf(sql.NullInt64{Int64: expires, Valid: true})
}

// markerTypes maps the event types that mark a task to the marker types.
var markerTypes = map[string]string{
	"milestone":      MarkerMilestone,
	"ask_raised":     MarkerAsk,
	"check_recorded": MarkerCheck,
	"task_done":      MarkerDone,
	"task_closed":    MarkerClosed,
}

// markerEvents are the event types of markerTypes.
var markerEvents = []string{"milestone", "ask_raised", "check_recorded", "task_done", "task_closed"}

// labelKey is the key of the event's data that holds a marker's label, for the
// types that have one; done and closed use the event's own summary.
var labelKey = map[string]string{"milestone": "summary", "ask_raised": "question", "check_recorded": "summary"}

// markers reads the marker events of the kept tasks inside [from, to], by time
// then event.
func (s Store) markers(ctx context.Context, f *filter, from, to time.Time) ([]Marker, error) {
	out := []Marker{}
	args := append([]any{}, f.args...)
	for _, name := range markerEvents {
		args = append(args, name)
	}
	args = append(args, store.EncodeTime(from), store.EncodeTime(to))
	err := s.each(ctx, "markers", `SELECT i.slug, t.seq, e.type, e.at, e.summary, e.data
		FROM events e JOIN tasks t ON t.id = e.task_id JOIN initiatives i ON i.id = t.initiative_id`+
		f.where()+` AND e.type IN (`+marks(len(markerEvents))+`) AND e.at >= ? AND e.at <= ? ORDER BY e.at, e.n`, args, func(r *sql.Rows) error {
		var slug, typ, summary, data string
		var seq int
		var at int64
		if err := r.Scan(&slug, &seq, &typ, &at, &summary, &data); err != nil {
			return err
		}
		out = append(out, Marker{Task: domain.FormatTaskRef(slug, seq), Type: markerTypes[typ], At: store.DecodeTime(at), Label: label(typ, summary, data)})
		return nil
	})
	return out, err
}

// label is the text of a marker: the data's summary or question where the type
// has one and the data holds it, the event's summary otherwise.
func label(typ, summary, data string) string {
	key, ok := labelKey[typ]
	if !ok {
		return summary
	}
	var body map[string]any
	if json.Unmarshal([]byte(data), &body) != nil {
		return summary
	}
	if text, ok := body[key].(string); ok && text != "" {
		return text
	}
	return summary
}
