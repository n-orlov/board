package leases

import (
	"context"

	"github.com/n-orlov/board/internal/store"
)

// LiveOf lists the live leases on the task whose id is taskID, oldest first: those
// in state live whose expires_at is not before now. A lease the sweeper has not
// marked yet but whose time has run out is not live, so it is left out; the list
// is empty, never nil, when the task has none. It only reads.
func (s Store) LiveOf(ctx context.Context, taskID string) ([]Lease, error) {
	return s.readLeases(ctx, selectLease+` WHERE l.task_id = ? AND l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`,
		[]any{taskID, store.EncodeTime(s.now())})
}
