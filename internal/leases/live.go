package leases

import (
	"context"

	"github.com/n-orlov/board/internal/store"
)

// LiveOf lists the live leases on the task whose id is taskID, oldest first: those
// in state live whose expires_at is not before now. It first expires the due
// leases (SPEC §5.1), so a lease whose time has run out is marked expired, with
// its one lease_expired event, and left out; the list is empty, never nil, when
// the task has none.
func (s Store) LiveOf(ctx context.Context, taskID string) ([]Lease, error) {
	if _, err := s.ExpireDue(ctx); err != nil {
		return nil, err
	}
	return s.readLeases(ctx, selectLease+` WHERE l.task_id = ? AND l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`,
		[]any{taskID, store.EncodeTime(s.now())})
}
