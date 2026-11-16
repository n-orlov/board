package checkin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/leases"
)

// BlockResult is what a composite block answers: the ask it raised and the
// caller's lease on its task that the lease action acted on, nil for the action
// none and for a caller that held no live lease: ended by stop, still live for
// keep.
type BlockResult struct {
	Ask   asks.Ask
	Lease *leases.Lease
}

// Block raises the ask in (SPEC 8.4) and, by its LeaseAction, ends or keeps the
// actor's live lease on the ask's task. With stop it ends the lease with the
// outcome `blocked on A<n>`; the ask, its event and the lease end with its event
// are one transaction. With keep it leaves the lease live and answers it. With
// none it only raises the ask. An action on a caller that holds no live lease on
// the task, or on an ask with no task, still raises the ask and acts on nothing.
// It fails as asks.Store.Raise does, with asks.ErrInvalid for a lease_action
// that is none of none, keep and stop (before anything is written). Stop and
// keep need Leases and Asks, none needs Asks.
func (s Store) Block(ctx context.Context, actor audit.Actor, principal string, in asks.New) (BlockResult, error) {
	in, err := in.Normalize()
	if err != nil {
		return BlockResult{}, err
	}
	if in.LeaseAction == domain.LeaseActionNone {
		k, err := s.Asks.Raise(ctx, actor, principal, in)
		return BlockResult{Ask: k}, err
	}
	tx, err := s.Leases.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return BlockResult{}, fmt.Errorf("begin: %w", err)
	}
	out, err := s.blockIn(ctx, tx, actor, principal, in)
	if err != nil {
		_ = tx.Rollback()
		return BlockResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return BlockResult{}, fmt.Errorf("commit: %w", err)
	}
	if out.Lease != nil {
		l, err := s.Leases.Get(ctx, out.Lease.Ref)
		out.Lease = &l
		return out, err
	}
	return out, nil
}

// blockIn raises the ask in tx, then ends or keeps the lease the actor holds on
// its task, as it was held.
func (s Store) blockIn(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, in asks.New) (BlockResult, error) {
	k, err := s.Asks.RaiseIn(ctx, tx, actor, principal, in)
	if err != nil || k.Task == "" {
		return BlockResult{Ask: k}, err
	}
	held, err := s.Leases.HeldIn(ctx, tx, actor.Handle, k.Task)
	var none leases.ConflictError
	if errors.As(err, &none) {
		return BlockResult{Ask: k}, nil
	}
	if err != nil {
		return BlockResult{}, err
	}
	if in.LeaseAction == domain.LeaseActionStop {
		err = s.Leases.EndHeldIn(ctx, tx, actor, principal, held, "blocked on "+k.Ref)
	}
	return BlockResult{Ask: k, Lease: &held}, err
}
