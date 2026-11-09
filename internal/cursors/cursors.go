package cursors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// The two streams a cursor can follow.
const (
	StreamNotes  = "notes"
	StreamEvents = "events"
)

// Errors a caller tells apart.
var (
	ErrInvalid  = errors.New("invalid cursor position")
	ErrNotFound = errors.New("cursor stream not found")
)

// InvalidError is a position that names no note or event. It satisfies
// errors.Is(err, ErrInvalid), its text is the sentence to show the caller and
// Next says what to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// Cursor is one cursor as set: the stream, the ULID of the last item read and
// that item's short ref (N<n> or E<n>).
type Cursor struct {
	Stream   string
	Position string
	Ref      string
}

// stream is what differs between the two streams: the table the position names
// a row of, how a short ref is read and written, and what to send on a refusal.
type stream struct {
	table  string
	parse  func(string) (int64, bool)
	format func(int64) string
	next   string
}

var streams = map[string]stream{
	StreamNotes: {table: "notes", parse: domain.ParseNoteRef, format: domain.FormatNoteRef,
		next: "Send the id of a note or an N<n> ref such as N40."},
	StreamEvents: {table: "events", parse: domain.ParseEventRef, format: domain.FormatEventRef,
		next: "Send the id of an event or an E<n> ref such as E42."},
}

// Store reads and writes the cursors of one database.
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

// lookup returns the stream called name, or ErrNotFound.
func lookup(name string) (stream, error) {
	st, ok := streams[name]
	if !ok {
		return stream{}, fmt.Errorf("%w: stream %s", ErrNotFound, name)
	}
	return st, nil
}

// Set moves the actor's cursor on the stream to the note or event position
// names (an N<n> or E<n> ref, or the ULID, in either letter case) and returns
// the cursor as stored: it holds the ULID. It returns ErrNotFound for a stream
// that is neither notes nor events and ErrInvalid for a position that names no
// note or event of that stream.
func (s Store) Set(ctx context.Context, actor audit.Actor, name, position string) (Cursor, error) {
	st, err := lookup(name)
	if err != nil {
		return Cursor{}, err
	}
	id, n, err := s.resolve(ctx, st, position)
	if err != nil {
		return Cursor{}, err
	}
	if err := s.Ack(ctx, actor, name, id); err != nil {
		return Cursor{}, err
	}
	return Cursor{Stream: name, Position: id, Ref: st.format(n)}, nil
}

// resolve returns the ULID and number of the row of the stream's table that
// position names.
func (s Store) resolve(ctx context.Context, st stream, position string) (string, int64, error) {
	position = strings.TrimSpace(position)
	query, arg := `SELECT id, n FROM `+st.table+` WHERE id = ?`, any(strings.ToUpper(position))
	if n, ok := st.parse(position); ok {
		query, arg = `SELECT id, n FROM `+st.table+` WHERE n = ?`, n
	}
	var (
		id string
		n  int64
	)
	err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(query), arg).Scan(&id, &n)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", 0, InvalidError{Msg: fmt.Sprintf("position %q names no %s", position, strings.TrimSuffix(st.table, "s")), Next: st.next}
	case err != nil:
		return "", 0, fmt.Errorf("read %s: %w", st.table, err)
	}
	return id, n, nil
}

// Ack records id, the ULID of a note or event that exists, as the actor's
// position on the stream, creating the actor on first use. It is the write
// `ack=true` makes; it returns ErrNotFound for an unknown stream.
func (s Store) Ack(ctx context.Context, actor audit.Actor, name, id string) error {
	if _, err := lookup(name); err != nil {
		return err
	}
	tx, err := s.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := s.write(ctx, tx, actor, name, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s Store) write(ctx context.Context, tx *sql.Tx, actor audit.Actor, name, id string) error {
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, s.now())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Upsert("cursors", []string{"actor_id", "stream", "position"},
		[]string{"actor_id", "stream"}, []string{"position"})), actorID, name, id)
	if err != nil {
		return fmt.Errorf("set cursor: %w", err)
	}
	return nil
}

// Get returns the position of the actor with this handle on the stream: the
// ULID of the last item it read, or "" when no cursor is set (so a read from
// the cursor starts at the beginning). It returns ErrNotFound for an unknown
// stream.
func (s Store) Get(ctx context.Context, handle, name string) (string, error) {
	if _, err := lookup(name); err != nil {
		return "", err
	}
	var position string
	err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(
		`SELECT c.position FROM cursors c JOIN actors a ON a.id = c.actor_id WHERE a.handle = ? AND c.stream = ?`),
		strings.TrimSpace(handle), name).Scan(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read cursor: %w", err)
	}
	return position, nil
}
