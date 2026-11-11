package leases

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// End ends the live lease ref names, with the optional outcome, and appends a
// lease_ended event. ended_by is set only when the actor is not the holder. It
// returns ErrInvalid for an outcome longer than 200 characters, ErrNotFound for
// an unknown lease and ErrConflict for a lease that is not live (ended or
// expired), and writes nothing then.
func (s Store) End(ctx context.Context, actor audit.Actor, principal, ref, outcome string) (Lease, error) {
	outcome = strings.TrimSpace(outcome)
	if outcome != "" {
		if err := domain.ValidateSummary(outcome); err != nil {
			return Lease{}, invalid(err, "Send an outcome of at most 200 characters, or leave it out.")
		}
	}
	var out Lease
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if _, err = s.expireDue(ctx, tx, s.now()); err != nil {
			return err
		}
		out, err = s.end(ctx, tx, actor, principal, ref, outcome)
		return err
	})
	return out, err
}

func (s Store) end(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, outcome string) (Lease, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Lease{}, err
	}
	now := s.now()
	if cur.State != domain.LeaseLive || cur.Expired(now) {
		return Lease{}, ConflictError{Msg: "cannot end lease " + cur.Ref + ": it is " + stateAt(cur, now),
			Next: "Only a live lease can be ended."}
	}
	actorID, err := audit.UpsertActor(ctx, tx, s.Store.Dialect(), actor, now)
	if err != nil {
		return Lease{}, err
	}
	if err := s.writeEnd(ctx, tx, actorID, principal, cur, outcome, actor.Handle, now, actorID != cur.ActorID); err != nil {
		return Lease{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// writeEnd ends cur, recording the ending actor as ended_by (and its handle in
// the event data) only when recordBy is set, and appends the lease_ended event.
func (s Store) writeEnd(ctx context.Context, tx *sql.Tx, actorID, principal string, cur Lease, outcome, handle string, now time.Time, recordBy bool) error {
	var endedBy any
	if recordBy {
		endedBy = actorID
	}
	_, err := tx.ExecContext(ctx, s.Store.Dialect().Rebind(`UPDATE leases SET state = 'ended', ended_at = ?, ended_by = ?, outcome = ? WHERE id = ?`),
		store.EncodeTime(now), endedBy, nullable(outcome), cur.ID)
	if err != nil {
		return fmt.Errorf("end lease: %w", err)
	}
	return s.appendFor(ctx, tx, actorID, principal, cur, now, EventEnded, "ended", endedData(cur, outcome, endedBy != nil, handle))
}

// endedData is the data of a lease_ended event.
func endedData(cur Lease, outcome string, byOther bool, by string) map[string]any {
	data := map[string]any{"ref": cur.Ref, "task": cur.Task}
	if outcome != "" {
		data["outcome"] = outcome
	}
	if byOther {
		data["ended_by"] = strings.TrimSpace(by)
	}
	return data
}

// stateAt is the state the lease is in at now: live past its expiry reads as expired.
func stateAt(l Lease, now time.Time) string {
	if l.Expired(now) {
		return domain.LeaseExpired
	}
	return l.State
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// appendFor appends a lease event on the task of cur, made by the actor row actorID.
func (s Store) appendFor(ctx context.Context, tx *sql.Tx, actorID, principal string, cur Lease, now time.Time, typ, verb string, data map[string]any) error {
	var initiative string
	err := tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT initiative_id FROM tasks WHERE id = ?`), cur.TaskID).Scan(&initiative)
	if err != nil {
		return fmt.Errorf("read task: %w", err)
	}
	summary := cur.Ref + " " + verb + " on " + cur.Task
	if o, ok := data["outcome"].(string); ok {
		summary += ": " + domain.OneLine(o)
	}
	return audit.AppendAs(ctx, tx, s.Store.Dialect(), actorID, now, audit.Event{
		Principal: principal, Type: typ, InitiativeID: initiative, TaskID: cur.TaskID, RefID: cur.ID, Summary: summary, Data: data,
	})
}

// Ack records the acknowledgement of the expired lease ref names: acked_at and
// acked_by, and a lease_acked event. Acknowledging a lease that is already
// acknowledged changes nothing and writes no event. It returns ErrNotFound for
// an unknown lease and ErrConflict for a lease that has not expired (live or
// ended), and writes nothing then.
func (s Store) Ack(ctx context.Context, actor audit.Actor, principal, ref string) (Lease, error) {
	var out Lease
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if _, err = s.expireDue(ctx, tx, s.now()); err != nil {
			return err
		}
		out, err = s.ack(ctx, tx, actor, principal, ref)
		return err
	})
	return out, err
}

func (s Store) ack(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string) (Lease, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Lease{}, err
	}
	now := s.now()
	if !cur.Expired(now) {
		return Lease{}, ConflictError{Msg: "cannot acknowledge lease " + cur.Ref + ": it is " + stateAt(cur, now),
			Next: "Only an expired lease can be acknowledged."}
	}
	if cur.AckedAt != nil {
		return cur, nil
	}
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return Lease{}, err
	}
	if _, err := tx.ExecContext(ctx, d.Rebind(`UPDATE leases SET acked_at = ?, acked_by = ? WHERE id = ?`), store.EncodeTime(now), actorID, cur.ID); err != nil {
		return Lease{}, fmt.Errorf("acknowledge lease: %w", err)
	}
	data := map[string]any{"ref": cur.Ref, "task": cur.Task}
	if err := s.appendFor(ctx, tx, actorID, principal, cur, now, EventAcked, "acknowledged", data); err != nil {
		return Lease{}, err
	}
	return s.find(ctx, tx, cur.ID)
}
