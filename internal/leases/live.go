package leases

import (
	"context"
	"fmt"

	"github.com/n-orlov/board/internal/store"
)

// LiveOf lists the live leases on the task whose id is taskID, oldest first: those
// in state live whose expires_at is not before now. A lease the sweeper has not
// marked yet but whose time has run out is not live, so it is left out; the list
// is empty, never nil, when the task has none. It only reads.
func (s Store) LiveOf(ctx context.Context, taskID string) ([]Lease, error) {
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(
		selectLease+` WHERE l.task_id = ? AND l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`), taskID, store.EncodeTime(s.now()))
	if err != nil {
		return nil, fmt.Errorf("read live leases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Lease{}
	for rows.Next() {
		l, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("read live leases: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read live leases: %w", err)
	}
	// Resources are read after the cursor is closed: the reader may be one connection.
	for i := range out {
		if out[i].Resources, err = s.resourcesOf(ctx, s.Store.Reader(), out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}
