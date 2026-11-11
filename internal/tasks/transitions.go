package tasks

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

// transition is one status change: the verbs of the three entry points.
type transition struct {
	action string
	event  string
	// allowed are the statuses the task may be in.
	allowed []string
	// set is the UPDATE's SET clause, with its placeholders filled by args.
	set  string
	args func(actorID string, at int64) []any
	// data is the extra event data.
	data map[string]any
	// behalf is set when the event records the actor the request is made for.
	behalf bool
	// leases settles the task's live leases in the transition's transaction; nil
	// leaves them as they are.
	leases leaseRule
}

// DoneOptions is the optional part of a done request. Outcome is the outcome the
// caller's own live lease on the task ends with ("" is "done"). Force ends the
// live leases of other actors too, instead of refusing.
type DoneOptions struct {
	Outcome string
	Force   bool
}

// Done marks the open task ref names as done, setting done_at and done_by, and
// appends a task_done event, in one transaction. A live lease the caller holds
// on the task ends in the same transaction (lease_ended, outcome opt.Outcome,
// else "done"). A live lease of another actor refuses the request with
// LeaseHeldError unless opt.Force is set, which ends every such lease with
// ended_by the caller and the outcome "ended: task marked done by <handle>". It
// returns ErrInvalid for an outcome longer than 200 characters (checked before
// the task is read), ErrNotFound for an unknown task and ErrConflict (naming
// the current status) for a task that is not open, and writes no row or event
// then.
func (s Store) Done(ctx context.Context, actor audit.Actor, principal, ref string, opt DoneOptions) (Task, error) {
	outcome := strings.TrimSpace(opt.Outcome)
	if outcome == "" {
		outcome = "done"
	} else if err := domain.ValidateSummary(outcome); err != nil {
		return Task{}, InvalidError{Msg: err.Error(), Next: "Send an outcome of at most 200 characters, or leave it out."}
	}
	return s.transit(ctx, actor, principal, ref, transition{
		action: "mark done", event: EventDone, allowed: []string{"open"},
		set:    `status = 'done', done_at = ?, done_by = ?`,
		args:   func(actorID string, at int64) []any { return []any{at, actorID} },
		leases: doneLeases(outcome, opt.Force),
	})
}

// Reopen moves a done or closed task back to open, clearing resolution, done_at,
// done_by, closed_at and closed_by, and appends a task_reopened event. It
// returns ErrNotFound for an unknown task, ErrConflict (naming the current
// status) for an open one and ErrInitiativeClosed when its initiative is
// closed, and writes no row or event then.
func (s Store) Reopen(ctx context.Context, actor audit.Actor, principal, ref string) (Task, error) {
	return s.transit(ctx, actor, principal, ref, transition{
		action: "reopen", event: EventReopened, allowed: []string{"done", "closed"},
		set:  `status = 'open', resolution = NULL, done_at = NULL, done_by = NULL, closed_at = NULL, closed_by = NULL`,
		args: func(string, int64) []any { return nil },
	})
}

// Close moves an open or done task to closed with the resolution (completed,
// dropped, superseded or duplicate), setting closed_at and closed_by, and
// appends a task_closed event whose data.resolution is the resolution. It
// returns ErrInvalid for a missing or unknown resolution (checked before the
// task is read), ErrNotFound for an unknown task and ErrConflict (naming the
// current status) for a closed one, and writes no row or event then. A done
// task keeps its done_at and done_by. Every live lease on the task ends with
// it (lease_ended, ended_by the caller when not the holder, outcome "ended: task
// closed by <handle>").
func (s Store) Close(ctx context.Context, actor audit.Actor, principal, ref, resolution string) (Task, error) {
	if err := domain.ValidateResolution(resolution); err != nil {
		return Task{}, InvalidError{Msg: err.Error(), Next: "Send resolution completed, dropped, superseded or duplicate."}
	}
	return s.transit(ctx, actor, principal, ref, closeTransition(resolution))
}

// closeTransition is the close of a task with the resolution. Every live lease
// on the task ends with it.
func closeTransition(resolution string) transition {
	return transition{
		action: "close", event: EventClosed, allowed: []string{"open", "done"},
		set:  `status = 'closed', resolution = ?, closed_at = ?, closed_by = ?`,
		args: func(actorID string, at int64) []any { return []any{resolution, at, actorID} },
		data: map[string]any{"resolution": resolution}, behalf: true, leases: closeLeases,
	}
}

func (s Store) transit(ctx context.Context, actor audit.Actor, principal, ref string, tr transition) (Task, error) {
	var out Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.transitInTx(ctx, tx, actor, principal, ref, tr)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (s Store) transitInTx(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, tr transition) (Task, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Task{}, err
	}
	if err := s.permit(ctx, tx, cur, tr); err != nil {
		return Task{}, err
	}
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return Task{}, err
	}
	if tr.leases != nil {
		if err := tr.leases(ctx, s, tx, leaseCall{task: cur, actor: actor, actorID: actorID, principal: principal, now: now}); err != nil {
			return Task{}, err
		}
	}
	onBehalf, err := behalfOf(ctx, tx, d, actor, now, tr)
	if err != nil {
		return Task{}, err
	}
	at := store.EncodeTime(now)
	args := append(tr.args(actorID, at), at, cur.ID)
	if _, err := tx.ExecContext(ctx, d.Rebind(`UPDATE tasks SET `+tr.set+`, updated_at = ? WHERE id = ?`), args...); err != nil {
		return Task{}, fmt.Errorf("%s task: %w", tr.action, err)
	}
	if err := s.appendTransition(ctx, tx, actorID, onBehalf, principal, cur, now, tr); err != nil {
		return Task{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// behalfOf is the actor row the transition's event is made on behalf of, or "".
func behalfOf(ctx context.Context, tx *sql.Tx, d store.Dialect, actor audit.Actor, now time.Time, tr transition) (string, error) {
	if !tr.behalf {
		return "", nil
	}
	return audit.OnBehalf(ctx, tx, d, actor, now)
}

// permit returns an error unless the transition may leave the task's current
// status, and, for a reopen, unless its initiative is open.
func (s Store) permit(ctx context.Context, tx *sql.Tx, cur Task, tr transition) error {
	if !contains(tr.allowed, cur.Status) {
		return ConflictError{Ref: cur.Ref, Status: cur.Status, Action: tr.action}
	}
	if tr.event != EventReopened {
		return nil
	}
	return s.writable(ctx, tx, Task{InitiativeID: cur.InitiativeID, Initiative: cur.Initiative})
}

func (s Store) appendTransition(ctx context.Context, tx *sql.Tx, actorID, onBehalf, principal string, cur Task, now time.Time, tr transition) error {
	data := map[string]any{"ref": cur.Ref, "from": cur.Status}
	summary := "task " + cur.Ref + " " + eventVerb[tr.event]
	if r, ok := tr.data["resolution"]; ok {
		data["resolution"] = r
		summary += " as " + r.(string)
	}
	return audit.AppendAs(ctx, tx, s.Store.Dialect(), actorID, now, audit.Event{
		Principal: principal, OnBehalfOfID: onBehalf, Type: tr.event, InitiativeID: cur.InitiativeID, TaskID: cur.ID, RefID: cur.ID,
		Summary: summary,
		Data:    data,
	})
}

var eventVerb = map[string]string{EventDone: "marked done", EventReopened: "reopened", EventClosed: "closed"}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
