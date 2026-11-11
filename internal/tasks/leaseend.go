package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/leases"
)

// leaseCall is what a lease rule needs to settle the leases of a task that is
// changing status: the task, the caller (as its actor and its actor row) and
// the instant.
type leaseCall struct {
	task      Task
	actor     audit.Actor
	actorID   string
	principal string
	now       time.Time
}

// leaseRule settles the live leases of a task in the transition's transaction.
type leaseRule func(ctx context.Context, s Store, tx *sql.Tx, c leaseCall) error

// LeaseHeld is one live lease that stops a task being marked done.
type LeaseHeld struct{ Ref, Holder, Intent string }

// LeaseHeldError is a done request refused because other actors hold live
// leases on the task. It satisfies errors.Is(err, ErrConflict).
type LeaseHeldError struct {
	Ref     string
	Holders []LeaseHeld
}

// Error names the task and each holder with the lease and its intent.
func (e LeaseHeldError) Error() string {
	parts := make([]string, len(e.Holders))
	for i, h := range e.Holders {
		parts[i] = fmt.Sprintf("%s holds lease %s (%s)", h.Holder, h.Ref, h.Intent)
	}
	return "cannot mark task " + e.Ref + " done: " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrConflict) true for every LeaseHeldError.
func (e LeaseHeldError) Is(target error) bool { return target == ErrConflict }

// Next is the step that clears the refusal: wait for the first lease, or force.
func (e LeaseHeldError) Next() string {
	return "wait for " + e.Holders[0].Ref + " to end, or --force"
}

// doneLeases ends the caller's own live lease with outcome; without force a
// lease of another actor refuses the done. With force every live lease on the
// task ends, the caller's own included, with ended_by the caller and the
// outcome `ended: task marked done by <handle>`.
func doneLeases(outcome string, force bool) leaseRule {
	return func(ctx context.Context, s Store, tx *sql.Tx, c leaseCall) error {
		live, err := s.leaseStore().LiveOnTaskIn(ctx, tx, c.task.ID, c.now)
		if err != nil {
			return err
		}
		if force {
			return endAll(ctx, s, tx, c, live, "ended: task marked done by "+c.actor.Handle, true)
		}
		if others := heldByOthers(live, c.actorID); len(others) > 0 {
			return LeaseHeldError{Ref: c.task.Ref, Holders: others}
		}
		return endAll(ctx, s, tx, c, live, outcome, false)
	}
}

// endAll ends each lease of live with outcome, recording the caller as
// ended_by when byCaller is set.
func endAll(ctx context.Context, s Store, tx *sql.Tx, c leaseCall, live []leases.Lease, outcome string, byCaller bool) error {
	ls := s.leaseStore()
	for _, l := range live {
		if err := ls.EndIn(ctx, tx, c.actorID, c.principal, l, outcome, c.actor.Handle, c.now, byCaller); err != nil {
			return err
		}
	}
	return nil
}

// heldByOthers lists the leases of live that an actor other than actorID holds.
func heldByOthers(live []leases.Lease, actorID string) []LeaseHeld {
	var held []LeaseHeld
	for _, l := range live {
		if l.ActorID != actorID {
			held = append(held, LeaseHeld{Ref: l.Ref, Holder: l.Actor, Intent: l.Intent})
		}
	}
	return held
}

// closeLeases ends every live lease on the task being closed, ended_by the
// caller and outcome `ended: task closed by <handle>`.
func closeLeases(ctx context.Context, s Store, tx *sql.Tx, c leaseCall) error {
	live, err := s.leaseStore().LiveOnTaskIn(ctx, tx, c.task.ID, c.now)
	if err != nil {
		return err
	}
	return endAll(ctx, s, tx, c, live, "ended: task closed by "+c.actor.Handle, true)
}

// leaseStore is the lease store of the same database and clock.
func (s Store) leaseStore() leases.Store { return leases.Store{Store: s.Store, Now: s.Now} }
