package initiatives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/n-orlov/village-hall/internal/audit"
)

// Event types a close and a reopen append.
const (
	EventClosed   = "initiative_closed"
	EventReopened = "initiative_reopened"
)

// ErrConflict is a close or reopen the initiative's state does not allow.
var ErrConflict = errors.New("initiative status conflict")

// ConflictError is a close or reopen refused. It satisfies errors.Is(err,
// ErrConflict), its text is the sentence to show the caller and Next says what
// to send instead.
type ConflictError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e ConflictError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrConflict) true for every ConflictError.
func (e ConflictError) Is(target error) bool { return target == ErrConflict }

// FindIn is Get reading through tx.
func (s Store) FindIn(ctx context.Context, tx *sql.Tx, ref string) (Initiative, error) {
	return s.find(ctx, tx, ref)
}

// AlreadyClosed is the refusal to close the closed initiative.
func AlreadyClosed(slug string) ConflictError {
	return ConflictError{Msg: "initiative " + slug + " is already closed",
		Next: "Reopen it with POST /api/v1/initiatives/" + slug + "/reopen if it needs more work."}
}

// MarkClosed sets the initiative's status to closed by actorID at now in tx. It
// is ConflictError when the initiative is not open, and holds the row until tx
// ends, so a task cannot be created in it meanwhile.
func (s Store) MarkClosed(ctx context.Context, tx *sql.Tx, ini Initiative, actorID string, now int64) error {
	res, err := tx.ExecContext(ctx, s.Store.Dialect().Rebind(
		`UPDATE initiatives SET status = 'closed', closed_at = ?, closed_by = ? WHERE id = ? AND status = 'open'`), now, actorID, ini.ID)
	if err != nil {
		return fmt.Errorf("close initiative: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return AlreadyClosed(ini.Slug)
	}
	return nil
}

// Reopen moves the closed initiative ref names back to open, clearing closed_at
// and closed_by, and appends an initiative_reopened event, in one transaction.
// Its tasks stay as they are. It returns ErrNotFound for an unknown initiative
// and ErrConflict for an open one, and writes nothing then.
func (s Store) Reopen(ctx context.Context, actor audit.Actor, principal, ref string) (Initiative, error) {
	var id string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.reopen(ctx, tx, actor, principal, ref)
		return err
	})
	if err != nil {
		return Initiative{}, err
	}
	return s.getBy(ctx, "i.id", id)
}

func (s Store) reopen(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string) (string, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return "", err
	}
	d := s.Store.Dialect()
	now := s.now()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, d.Rebind(
		`UPDATE initiatives SET status = 'open', closed_at = NULL, closed_by = NULL WHERE id = ? AND status = 'closed'`), cur.ID)
	if err != nil {
		return "", fmt.Errorf("reopen initiative: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return "", ConflictError{Msg: "initiative " + cur.Slug + " is open",
			Next: "Only a closed initiative can be reopened. Read it with GET /api/v1/initiatives/" + cur.Slug + "."}
	}
	return cur.ID, audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventReopened, InitiativeID: cur.ID, RefID: cur.ID,
		Summary: "initiative " + cur.Slug + " reopened",
		Data:    map[string]any{"slug": cur.Slug},
	})
}
