package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

// States a notification row is in, and the values of the state filter.
const (
	StatePending = "pending"
	StateSent    = "sent"
	StateFailed  = "failed"
)

// ErrInvalid is a query the caller got wrong.
var ErrInvalid = errors.New("invalid notifications query")

// InvalidError is a parameter the query got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// Notification is one row of the outbox, as the API shows it. Event is the E<n>
// ref of the event that enqueued it. LastError, SentAt and NextAttemptAt are
// zero when the row has none (NextAttemptAt is kept only while it is pending).
type Notification struct {
	ID            string
	Kind          string
	State         string
	Event         string
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
	SentAt        time.Time
	Payload       map[string]any
}

// Query selects a page of notifications, newest first. State is pending, sent or
// failed, nil for all (a State that points at an empty string is not a state). After is the id of the last row of the previous page;
// Limit 0 is the default page size.
type Query struct {
	State *string
	After string
	Limit int
}

// Page is one page of notifications. NextAfter is the id to send as After for the
// following page, empty on the last page.
type Page struct {
	Items     []Notification
	NextAfter string
}

// Store reads the outbox of one database.
type Store struct{ Store *store.Store }

const selectNotification = `SELECT n.id, n.kind, n.state, e.n, n.attempts, n.next_attempt_at, n.last_error,
	n.created_at, n.sent_at, n.payload FROM notifications n JOIN events e ON e.id = n.event_id`

// List returns the page of notifications q selects, newest first by creation time
// and then id. It returns ErrInvalid for a state that is none of the three, a
// limit out of range or an after that names no row.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	size, err := domain.PageSize(q.Limit)
	if err != nil {
		return Page{}, InvalidError{Msg: err.Error(), Next: "Send a limit between 1 and 1000, or leave it out for 100."}
	}
	conds, args, err := s.conditions(ctx, q)
	if err != nil {
		return Page{}, err
	}
	query := selectNotification
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	items, err := s.rows(ctx, query+` ORDER BY n.created_at DESC, n.id DESC LIMIT `+fmt.Sprint(size+1), args)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: items}
	if len(items) > size {
		p.Items = items[:size]
		p.NextAfter = items[size-1].ID
	}
	return p, nil
}

func (s Store) conditions(ctx context.Context, q Query) ([]string, []any, error) {
	var conds []string
	var args []any
	if q.State != nil {
		switch *q.State {
		case StatePending, StateSent, StateFailed:
			conds, args = append(conds, `n.state = ?`), append(args, *q.State)
		default:
			return nil, nil, InvalidError{Msg: fmt.Sprintf("state %q is not a notification state", *q.State),
				Next: "Send state=pending, sent or failed, or leave state out for all."}
		}
	}
	if q.After != "" {
		at, id, err := s.cursor(ctx, q.After)
		if err != nil {
			return nil, nil, err
		}
		conds = append(conds, `(n.created_at < ? OR (n.created_at = ? AND n.id < ?))`)
		args = append(args, at, at, id)
	}
	return conds, args, nil
}

// cursor resolves the id of the last row of the previous page to its creation
// time, so the next page starts after it.
func (s Store) cursor(ctx context.Context, after string) (int64, string, error) {
	invalid := InvalidError{Msg: fmt.Sprintf("after %q is not a notification id", after),
		Next: "Send the next_after of the previous page."}
	id, ok := domain.KeysetULID(after)
	if !ok {
		return 0, "", invalid
	}
	var at int64
	err := s.Store.ReaderFor(ctx).QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT created_at FROM notifications WHERE id = ?`), id).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", invalid
	}
	if err != nil {
		return 0, "", fmt.Errorf("find the notification %s: %w", id, err)
	}
	return at, id, nil
}

func (s Store) rows(ctx context.Context, query string, args []any) ([]Notification, error) {
	rows, err := s.Store.ReaderFor(ctx).QueryContext(ctx, s.Store.Dialect().Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("read notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Notification{}
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("read notifications: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notifications: %w", err)
	}
	return out, nil
}

func scan(rows *sql.Rows) (Notification, error) {
	var (
		n                Notification
		eventN, next, at int64
		lastError        sql.NullString
		sentAt           sql.NullInt64
		payload          string
	)
	if err := rows.Scan(&n.ID, &n.Kind, &n.State, &eventN, &n.Attempts, &next, &lastError, &at, &sentAt, &payload); err != nil {
		return n, err
	}
	n.Event = fmt.Sprintf("E%d", eventN)
	n.CreatedAt = store.DecodeTime(at)
	if n.State == StatePending {
		n.NextAttemptAt = store.DecodeTime(next)
	}
	n.LastError = lastError.String
	if sentAt.Valid {
		n.SentAt = store.DecodeTime(sentAt.Int64)
	}
	if err := json.Unmarshal([]byte(payload), &n.Payload); err != nil {
		return n, fmt.Errorf("payload of %s: %w", n.ID, err)
	}
	return n, nil
}
