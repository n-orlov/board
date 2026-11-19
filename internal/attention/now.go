package attention

import (
	"context"
	"database/sql"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

// Windows a recently touched task may be inside of, and the one used when none is
// asked for.
const (
	Window8h      = "8h"
	Window24h     = "24h"
	Window3d      = "3d"
	DefaultWindow = Window24h
)

// NextWindow says what to send instead of a window that is not one of the three.
const NextWindow = "Send window=8h, window=24h or window=3d, or leave window out for 24h."

// WindowSpan is how long a window reaches back, and false for a value that is not
// a window.
func WindowSpan(window string) (time.Duration, bool) {
	switch window {
	case Window8h:
		return 8 * time.Hour, true
	case Window24h:
		return 24 * time.Hour, true
	case Window3d:
		return 72 * time.Hour, true
	}
	return 0, false
}

// FlightTask names the task an in-flight or recent item is about.
type FlightTask struct {
	Ref        string
	Title      string
	Initiative string
	Status     string
	Kind       string
}

// FlightLease is one live lease of an in-flight task.
type FlightLease struct {
	Lease     string
	Holder    Actor
	Intent    string
	StartedAt time.Time
	RenewedAt time.Time
	ExpiresAt time.Time
}

// InFlight is a task with a live lease and its live leases in lease-number order.
type InFlight struct {
	Task   FlightTask
	Leases []FlightLease
}

// LastEvent is the last event written on a task.
type LastEvent struct {
	Type    string
	Summary string
	Actor   Actor
	At      time.Time
}

// Recent is a task with no live lease whose last event is inside the window.
type Recent struct {
	Task      FlightTask
	LastEvent LastEvent
}

// Board is what the Now view shows: the attention sections, the tasks in flight
// (the newest lease first) and the recently touched ones (the latest event first).
type Board struct {
	Window    string
	Attention Result
	InFlight  []InFlight
	Recent    []Recent
}

// flightCols are the columns that name a task, in the order scanTask reads them.
const flightCols = `i.slug, t.seq, t.title, t.status, t.kind`

func scanTask(r rowScanner, into ...any) (FlightTask, error) {
	var (
		t   FlightTask
		seq int
	)
	if err := r.Scan(append([]any{&t.Initiative, &seq, &t.Title, &t.Status, &t.Kind}, into...)...); err != nil {
		return FlightTask{}, err
	}
	t.Ref = domain.FormatTaskRef(t.Initiative, seq)
	return t, nil
}

// Board reads the attention sections, the in-flight tasks and the recently
// touched ones for window, all narrowed by q. It returns InvalidError for a
// window that is not one of the three, ErrInvalid for a malformed tag and
// ErrNotFound for an initiative that names nothing. Whatever the size of the
// store it makes a constant number of queries.
func (s Store) Board(ctx context.Context, q Query, window string) (Board, error) {
	span, ok := WindowSpan(window)
	if !ok {
		return Board{}, InvalidError{Msg: "window " + window + " is not 8h, 24h or 3d", Next: NextWindow}
	}
	sc, now, err := s.begin(ctx, q)
	if err != nil {
		return Board{}, err
	}
	b := Board{Window: window}
	if b.Attention, err = s.get(ctx, sc, now); err != nil {
		return Board{}, err
	}
	if b.InFlight, err = s.inFlight(ctx, sc, now); err != nil {
		return Board{}, err
	}
	b.Recent, err = s.recent(ctx, sc, now, span)
	return b, err
}

func liveCond(now time.Time) (string, []any) {
	return `l.state = 'live' AND l.expires_at >= ?`, []any{store.EncodeBound(now)}
}

// inFlight lists the tasks with a live lease, each with its live leases, from one
// query over the leases: the task whose newest live lease is newest comes first.
func (s Store) inFlight(ctx context.Context, sc scope, now time.Time) ([]InFlight, error) {
	live, args := liveCond(now)
	cond, more := sc.and(`t.initiative_id`)
	sec := section{
		cols: flightCols + `, l.n, l.intent, l.started_at, l.renewed_at, l.expires_at, a.handle, a.way_back`,
		from: `leases l JOIN tasks t ON t.id = l.task_id JOIN initiatives i ON i.id = t.initiative_id
		 JOIN actors a ON a.id = l.actor_id WHERE ` + live + cond,
		order: `l.n DESC`, args: append(args, more...),
	}
	rows, err := collect(ctx, s, sec, func(r rowScanner) (flightRow, error) {
		var (
			n                        int64
			started, renewed, expiry int64
			wb                       sql.NullString
			l                        FlightLease
		)
		t, err := scanTask(r, &n, &l.Intent, &started, &renewed, &expiry, &l.Holder.Handle, &wb)
		l.Lease, l.Holder.WayBack = domain.FormatLeaseRef(n), wb.String
		l.StartedAt, l.RenewedAt, l.ExpiresAt = store.DecodeTime(started), store.DecodeTime(renewed), store.DecodeTime(expiry)
		return flightRow{task: t, lease: l}, err
	})
	return groupFlight(rows), err
}

type flightRow struct {
	task  FlightTask
	lease FlightLease
}

// groupFlight folds rows (newest lease first) into one item per task, in the order
// the tasks first appear, with each task's leases in lease-number order.
func groupFlight(rows []flightRow) []InFlight {
	out := []InFlight{}
	at := map[string]int{}
	for _, r := range rows {
		i, seen := at[r.task.Ref]
		if !seen {
			i = len(out)
			at[r.task.Ref] = i
			out = append(out, InFlight{Task: r.task, Leases: []FlightLease{}})
		}
		out[i].Leases = append([]FlightLease{r.lease}, out[i].Leases...)
	}
	return out
}

// recent lists the tasks with no live lease whose last event is not older than the
// window, the latest event first, from one query.
func (s Store) recent(ctx context.Context, sc scope, now time.Time, span time.Duration) ([]Recent, error) {
	live, args := liveCond(now)
	cond, more := sc.and(`t.initiative_id`)
	args = append([]any{store.EncodeBound(now.Add(-span))}, args...)
	sec := section{
		cols: flightCols + `, e.type, e.summary, e.at, a.handle, a.way_back`,
		from: `tasks t JOIN initiatives i ON i.id = t.initiative_id
		 JOIN events e ON e.n = (SELECT MAX(c.n) FROM events c WHERE c.task_id = t.id)
		 JOIN actors a ON a.id = e.actor_id
		 WHERE e.at >= ? AND NOT EXISTS (SELECT 1 FROM leases l WHERE l.task_id = t.id AND ` + live + `)` + cond,
		order: `e.at DESC, e.n DESC`, args: append(args, more...),
	}
	return collect(ctx, s, sec, func(r rowScanner) (Recent, error) {
		var (
			ev LastEvent
			at int64
			wb sql.NullString
		)
		t, err := scanTask(r, &ev.Type, &ev.Summary, &at, &ev.Actor.Handle, &wb)
		ev.Actor.WayBack, ev.At = wb.String, store.DecodeTime(at)
		return Recent{Task: t, LastEvent: ev}, err
	})
}
