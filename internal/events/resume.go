package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/n-orlov/village-hall/internal/domain"
)

// ResumeLimit is the most events a resuming stream replays: a client that missed
// more is told to refetch instead.
const ResumeLimit = 1000

// Resume is what a stream that reconnects with a Last-Event-ID is owed.
//
// Known is false when the id names no event (it is not an event ULID or E<n>
// ref, or no such event exists). TooMany is true when more than ResumeLimit
// events were written after it. Otherwise Items are the events after it, in
// order, up to the position asked for. From is the number of the event named by
// the id.
type Resume struct {
	Known   bool
	TooMany bool
	From    int64
	Items   []Event
}

// Resume reads the events a stream that last saw lastID has missed, up to and
// including the event numbered upTo (the position its live part starts after).
func (s Store) Resume(ctx context.Context, lastID string, upTo int64) (Resume, error) {
	from, found, err := s.number(ctx, lastID)
	if err != nil || !found {
		return Resume{}, err
	}
	r := Resume{Known: true, From: from}
	if from >= upTo {
		return r, nil
	}
	q := selectEvent + ` WHERE e.n > ? AND e.n <= ? ORDER BY e.n LIMIT ` + fmt.Sprint(ResumeLimit+1)
	r.Items, err = s.rows(ctx, q, []any{from, upTo})
	if err != nil {
		return Resume{}, err
	}
	if len(r.Items) > ResumeLimit {
		r.TooMany, r.Items = true, nil
	}
	return r, nil
}

// number returns the sequence number of the event an id (ULID or E<n> ref,
// either case) names, and whether there is such an event.
func (s Store) number(ctx context.Context, id string) (int64, bool, error) {
	p, err := domain.ParseEventPoint(id, true)
	if err != nil {
		return 0, false, nil //nolint:nilerr // an unreadable id is an unknown one, not a failure
	}
	query, arg := `SELECT n FROM events WHERE id = ?`, any(p.ID)
	if p.Kind == domain.PointN {
		query, arg = `SELECT n FROM events WHERE n = ?`, p.N
	}
	var n int64
	err = s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(query), arg).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find event: %w", err)
	}
	return n, true, nil
}
