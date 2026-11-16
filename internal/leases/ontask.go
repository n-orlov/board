package leases

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

// LiveOnTaskIn lists the live leases on the task taskID inside the transaction
// tx, oldest first, after expiring the ones that are due (each with its one
// lease_expired event), so a lease past its expiry is never listed. It is for
// a task transition that must see, and end, the leases in its own transaction.
func (s Store) LiveOnTaskIn(ctx context.Context, tx *sql.Tx, taskID string, now time.Time) ([]Lease, error) {
	if _, err := s.expireDue(ctx, tx, now); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, s.Store.Dialect().Rebind(selectLease+` WHERE l.task_id = ? AND l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`),
		taskID, store.EncodeBound(now))
	if err != nil {
		return nil, fmt.Errorf("read leases: %w", err)
	}
	out, err := scanAll(rows)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EndIn ends the live lease cur inside the transaction tx with the outcome and
// appends its lease_ended event. With byCaller, ended_by is the actor row
// actorID and the event data names handle, whoever holds the lease (a forced
// done, a close); without it neither is recorded (the holder's own done). The
// caller has read cur live in the same transaction.
func (s Store) EndIn(ctx context.Context, tx *sql.Tx, actorID, principal string, cur Lease, outcome, handle string, now time.Time, byCaller bool) error {
	return s.writeEnd(ctx, tx, actorID, principal, cur, outcome, handle, now, byCaller)
}
