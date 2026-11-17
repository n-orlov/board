package checkin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/leases"
)

// PingRequest is a composite ping (SPEC 8.4): the actor renews every live lease
// it holds and, with a Progress, writes it as a milestone on each leased task.
type PingRequest struct {
	Progress *string
}

// PingResult is what a composite ping answers: the leases it renewed, oldest
// first, and the refs of the tasks it wrote a milestone on (none without
// progress), one for each leased task.
type PingResult struct {
	Leases     []leases.Lease
	Milestones []string
}

// Ping renews every live lease of the actor and, with Progress, writes one
// milestone on each leased task. The renewals, the milestones and their events
// rows (lease_renewed, milestone) are one transaction: a request that fails
// anywhere writes nothing. It returns tasks.ErrInvalid for a progress that is
// not 1 to 200 characters (checked before anything is read) and
// leases.ErrConflict for an actor that holds no live lease. The request needs
// Leases and, with Progress, Tasks.
func (s Store) Ping(ctx context.Context, actor audit.Actor, principal string, in PingRequest) (PingResult, error) {
	if in.Progress != nil {
		p := strings.TrimSpace(*in.Progress)
		if err := domain.ValidateSummary(p); err != nil {
			return PingResult{}, leases.InvalidError{Msg: "the progress is wrong: " + err.Error(),
				Next: "Send progress of 1 to 200 characters, or leave it out to only renew the leases."}
		}
		in.Progress = &p
	}
	var out PingResult
	tx, err := s.Leases.Store.Begin(ctx)
	if err != nil {
		return PingResult{}, fmt.Errorf("begin: %w", err)
	}
	if out, err = s.ping(ctx, tx.Tx, actor, principal, in); err != nil {
		_ = tx.Rollback()
		return PingResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PingResult{}, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

// ping does the writes of a ping in tx.
func (s Store) ping(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, in PingRequest) (PingResult, error) {
	held, err := s.Leases.RenewHeldIn(ctx, tx, actor, principal)
	if err != nil {
		return PingResult{}, err
	}
	out := PingResult{Leases: held}
	if in.Progress == nil {
		return out, nil
	}
	for _, l := range held {
		t, err := s.Tasks.MilestoneIn(ctx, tx, actor, principal, l.Task, *in.Progress, nil)
		if err != nil {
			return PingResult{}, err
		}
		out.Milestones = append(out.Milestones, t.Ref)
	}
	return out, nil
}
