package notes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

// EventPosted is the type of the event every note appends, broadcast or not.
const EventPosted = "note_posted"

// Errors a caller tells apart.
var (
	ErrInvalid  = errors.New("invalid note")
	ErrNotFound = errors.New("note not found")
)

// InvalidError is a field the request got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(err error, next string) error {
	if err == nil {
		return nil
	}
	return InvalidError{Msg: err.Error(), Next: next}
}

// Note is one notes row. Ref is N<n>. Task is the task's ref and Initiative the
// initiative's slug, both empty when the note has none; Actor is the author's
// handle. Body is empty when the note has none, and also when it was listed
// without bodies; HasBody tells the two apart. Broadcast is derived: no task and no initiative.
type Note struct {
	N            int64
	ID           string
	Ref          string
	Actor        string
	ActorID      string
	Summary      string
	Body         string
	HasBody      bool
	TaskID       string
	Task         string
	InitiativeID string
	Initiative   string
	Resource     string
	Mentions     []string
	CreatedAt    time.Time
	Broadcast    bool
}

// Store reads and writes the notes of one database.
type Store struct {
	Store *store.Store
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

const selectNote = `SELECT k.n, k.id, a.handle, k.actor_id, k.summary, k.body, k.task_id, ti.slug, t.seq,
	k.initiative_id, i.slug, k.resource, k.mentions, k.created_at
	FROM notes k JOIN actors a ON a.id = k.actor_id
	LEFT JOIN tasks t ON t.id = k.task_id LEFT JOIN initiatives ti ON ti.id = t.initiative_id
	LEFT JOIN initiatives i ON i.id = k.initiative_id`

type rowScanner interface{ Scan(dest ...any) error }

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func scan(r rowScanner) (Note, error) {
	var (
		k                                  Note
		body, taskID, taskSlug, iniID, ini sql.NullString
		resource                           sql.NullString
		seq                                sql.NullInt64
		mentions                           string
		created                            int64
	)
	err := r.Scan(&k.N, &k.ID, &k.Actor, &k.ActorID, &k.Summary, &body, &taskID, &taskSlug, &seq, &iniID, &ini,
		&resource, &mentions, &created)
	if err != nil {
		return Note{}, err
	}
	k.Ref = domain.FormatNoteRef(k.N)
	k.Body, k.TaskID, k.InitiativeID, k.Initiative, k.Resource = body.String, taskID.String, iniID.String, ini.String, resource.String
	if taskID.Valid {
		k.Task = domain.FormatTaskRef(taskSlug.String, int(seq.Int64))
	}
	k.Mentions = []string{}
	if err := json.Unmarshal([]byte(mentions), &k.Mentions); err != nil {
		return Note{}, fmt.Errorf("read mentions: %w", err)
	}
	k.CreatedAt = store.DecodeTime(created)
	k.HasBody = body.Valid && body.String != ""
	k.Broadcast = !taskID.Valid && !iniID.Valid
	return k, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// find reads the note a ref (N<n>, either case, or its ULID in either case)
// names through q.
func (s Store) find(ctx context.Context, q queryer, ref string) (Note, error) {
	where, arg := ` WHERE k.n = ?`, any(nil)
	if n, ok := domain.ParseNoteRef(ref); ok {
		arg = n
	} else if up := strings.ToUpper(ref); domain.ValidateULID(up) == nil {
		where, arg = ` WHERE k.id = ?`, up
	} else {
		return Note{}, fmt.Errorf("%w: note %s", ErrNotFound, ref)
	}
	k, err := scan(q.QueryRowContext(ctx, s.Store.Dialect().Rebind(selectNote+where), arg))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Note{}, fmt.Errorf("%w: note %s", ErrNotFound, ref)
	case err != nil:
		return Note{}, fmt.Errorf("read note: %w", err)
	}
	return k, nil
}

// Get returns the note whose ref (N<n>) or ULID is ref, in either letter case,
// body included, or ErrNotFound.
func (s Store) Get(ctx context.Context, ref string) (Note, error) {
	return s.find(ctx, s.Store.Reader(), ref)
}

// inTx runs f in a transaction on the writer and commits when f returns nil.
func (s Store) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
