package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

const nextSummary = "Send a summary of 1 to 200 characters."

// Check records a check on the watch task ref names: a check_recorded event
// whose data is {ref, summary, next_check_at}, and next_check_at moved to
// nextCheck, or to now plus the task's cadence when nextCheck is nil, in one
// transaction. It returns ErrInvalid for a summary that is empty or longer than
// 200 characters (checked before the task is read), ErrNotFound for an unknown
// task and ErrConflict (naming the current status) for a work task or a closed
// one, and writes no row, event or actor then.
func (s Store) Check(ctx context.Context, actor audit.Actor, principal, ref, summary string, nextCheck *time.Time) (Task, error) {
	if err := domain.ValidateSummary(summary); err != nil {
		return Task{}, InvalidError{Msg: err.Error(), Next: nextSummary}
	}
	var out Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.checkInTx(ctx, tx, actor, principal, ref, summary, nextCheck)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (s Store) checkInTx(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, summary string, nextCheck *time.Time) (Task, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Task{}, err
	}
	if err := checkable(cur); err != nil {
		return Task{}, err
	}
	now := s.now()
	next := ms(nextCheck)
	if next == nil {
		n := now.Add(time.Duration(*cur.CadenceSeconds) * time.Second)
		next = &n
	}
	if err := s.writeCheck(ctx, tx, actor, principal, cur, summary, *next, now); err != nil {
		return Task{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// checkable returns a ConflictError unless t is a watch task that is not closed.
func checkable(t Task) error {
	status := t.Status
	if t.Kind != domain.KindWatch {
		status = "a work task"
	} else if status != "closed" {
		return nil
	}
	return ConflictError{Ref: t.Ref, Status: status, Action: "record a check on"}
}

// writeCheck moves next_check_at of cur to next and appends the check event.
func (s Store) writeCheck(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, cur Task, summary string, next, now time.Time) error {
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(`UPDATE tasks SET next_check_at = ?, updated_at = ? WHERE id = ?`),
		store.EncodeTime(next), store.EncodeTime(now), cur.ID)
	if err != nil {
		return fmt.Errorf("record check: %w", err)
	}
	return audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventCheck, InitiativeID: cur.InitiativeID, TaskID: cur.ID, RefID: cur.ID,
		Summary: "check on " + cur.Ref + ": " + domain.OneLine(summary),
		Data:    map[string]any{"ref": cur.Ref, "summary": summary, "next_check_at": next.Format(time.RFC3339Nano)},
	})
}

// Milestone records a milestone on the task ref names, of any kind and status:
// a milestone event whose data is {ref, summary} plus data (the caller's object,
// under the key "data") when data is not nil. The task row is not touched. It
// returns ErrInvalid for a summary that is empty or longer than 200 characters
// (checked before the task is read) and ErrNotFound for an unknown task, and
// writes no event or actor then.
func (s Store) Milestone(ctx context.Context, actor audit.Actor, principal, ref, summary string, data map[string]any) (Task, error) {
	if err := domain.ValidateSummary(summary); err != nil {
		return Task{}, InvalidError{Msg: err.Error(), Next: nextSummary}
	}
	var out Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := s.find(ctx, tx, ref)
		if err != nil {
			return err
		}
		out = cur
		return s.appendMilestone(ctx, tx, actor, principal, cur, summary, data)
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (s Store) appendMilestone(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, cur Task, summary string, data map[string]any) error {
	now := s.now()
	d := s.Store.Dialect()
	body := map[string]any{"ref": cur.Ref, "summary": summary}
	if data != nil {
		body["data"] = data
	}
	return audit.Append(ctx, tx, d, actor, now, audit.Event{
		Principal: principal, Type: EventMilestone, InitiativeID: cur.InitiativeID, TaskID: cur.ID, RefID: cur.ID,
		Summary: "milestone on " + cur.Ref + ": " + domain.OneLine(summary),
		Data:    body,
	})
}
