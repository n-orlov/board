package checkin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/notes"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// FinishRequest is a composite finish (SPEC 8.4): the actor ends its live lease
// on Task (its only live lease when Task is empty), writes Outcome as a note on
// the task and, with Done, marks the task done.
type FinishRequest struct {
	Task    string
	Outcome string
	Done    bool
}

// FinishResult is what a composite finish answers: the lease it ended, the note
// it wrote and whether it marked the task done.
type FinishResult struct {
	Lease leases.Lease
	Note  notes.Note
	Done  bool
}

// Finish ends the actor's live lease, writes the outcome as a note on its task
// and, with Done, marks the task done. The lease end, the note, the task status
// and their events rows (lease_ended, note_posted and, with Done, task_done) are
// one transaction: a request that fails anywhere writes nothing. The outcome is
// the lease's outcome and the note's summary. It returns leases.ErrInvalid for
// an outcome that is not 1 to 200 characters on one line, leases.ErrNotFound for
// an unknown task, leases.ErrConflict for an actor that holds no live lease (or
// several, with no task named) and, with Done, tasks.ErrConflict while another
// actor holds a live lease on the task. The request needs Leases, Notes and,
// with Done, Tasks.
func (s Store) Finish(ctx context.Context, actor audit.Actor, principal string, in FinishRequest) (FinishResult, error) {
	in.Outcome = strings.TrimSpace(in.Outcome)
	if err := domain.ValidateNoteSummary(in.Outcome); err != nil {
		return FinishResult{}, leases.InvalidError{Msg: "the outcome is wrong: " + err.Error(),
			Next: "Send an outcome of 1 to 200 characters on one line, saying what you did or where you left it."}
	}
	var ended leases.Lease
	var note notes.Note
	tx, err := s.Leases.Store.Begin(ctx)
	if err != nil {
		return FinishResult{}, fmt.Errorf("begin: %w", err)
	}
	if ended, note, err = s.finish(ctx, tx.Tx, actor, principal, in); err != nil {
		_ = tx.Rollback()
		return FinishResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FinishResult{}, fmt.Errorf("commit: %w", err)
	}
	ended, err = s.Leases.Get(ctx, ended.Ref)
	return FinishResult{Lease: ended, Note: note, Done: in.Done}, err
}

// finish does the writes of a finish in tx and returns the lease as it was held
// and the note.
func (s Store) finish(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, in FinishRequest) (leases.Lease, notes.Note, error) {
	held, err := s.Leases.HeldIn(ctx, tx, actor.Handle, in.Task)
	if err != nil {
		return leases.Lease{}, notes.Note{}, err
	}
	if in.Done {
		_, err = s.Tasks.DoneIn(ctx, tx, actor, principal, held.Task, tasks.DoneOptions{Outcome: in.Outcome})
	} else {
		err = s.Leases.EndHeldIn(ctx, tx, actor, principal, held, in.Outcome)
	}
	if err != nil {
		return leases.Lease{}, notes.Note{}, err
	}
	note, err := s.Notes.PostIn(ctx, tx, actor, principal, notes.New{Task: held.Task, Summary: in.Outcome})
	return held, note, err
}
