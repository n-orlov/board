package checkin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/cursors"
	"github.com/n-orlov/village-hall/internal/notes"
)

const (
	// EventCatchup is the event a catch-up that returned something appends. Its
	// data records where the answers of that catch-up ended (answers_through, an
	// event number), which is where the next one starts.
	EventCatchup = "catchup"
	// CatchupNotesLimit is the most notes one catch-up acknowledges.
	CatchupNotesLimit = 100
)

// CatchupResult is what a catch-up answers: the notes after the actor's notes
// cursor that it acknowledged (summaries, oldest first, at most
// CatchupNotesLimit), how many more there are, and the answers to the actor's
// asks since its previous catch-up, oldest first. Neither list is nil.
type CatchupResult struct {
	Notes     []notes.Note
	NotesMore int
	Answers   []asks.Ask
}

// Catchup reads what the actor missed (SPEC 8.4): the notes after its notes
// cursor, which it acknowledges by moving the cursor to the last one returned,
// and the answers to its asks given since its previous catch-up (in the last
// AnswerWindow when it never caught up). The cursor move and the catchup event
// are one transaction, so a failure leaves the cursor where it was. A catch-up
// that returns nothing writes nothing. The request needs Cursors, Notes and
// Asks.
func (s Store) Catchup(ctx context.Context, actor audit.Actor, principal string) (CatchupResult, error) {
	out, through, err := s.unread(ctx, actor.Handle)
	if err != nil {
		return CatchupResult{}, err
	}
	if len(out.Notes) == 0 && len(out.Answers) == 0 {
		return out, nil
	}
	return out, s.record(ctx, actor, principal, out, through)
}

// unread reads what the actor with this handle missed, and the number of the
// newest event it was read up to. It writes nothing.
func (s Store) unread(ctx context.Context, handle string) (CatchupResult, int64, error) {
	cursor, err := s.Cursors.Get(ctx, handle, cursors.StreamNotes)
	if err != nil {
		return CatchupResult{}, 0, err
	}
	out := CatchupResult{}
	if out.Notes, out.NotesMore, err = s.Notes.After(ctx, cursor, CatchupNotesLimit); err != nil {
		return CatchupResult{}, 0, err
	}
	through, err := s.answeredThrough(ctx)
	if err != nil {
		return CatchupResult{}, 0, err
	}
	out.Answers, err = s.answers(ctx, handle, through)
	return out, through, err
}

// answeredThrough is the number of the newest event, read before the answers
// so that every answer up to it is delivered now and none is skipped later.
func (s Store) answeredThrough(ctx context.Context) (int64, error) {
	var n int64
	err := s.Cursors.Store.Reader().QueryRowContext(ctx, `SELECT COALESCE(MAX(n), 0) FROM events`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("read newest event: %w", err)
	}
	return n, nil
}

// answers lists the answers to the actor's asks that its previous catch-up did
// not deliver, up to the event number through.
func (s Store) answers(ctx context.Context, handle string, through int64) ([]asks.Ask, error) {
	after, found, err := s.previousCatchup(ctx, handle)
	if err != nil {
		return nil, err
	}
	if found {
		return s.Asks.AnsweredBetween(ctx, handle, after, through)
	}
	return s.Asks.AnsweredSince(ctx, handle, s.now().Add(-AnswerWindow), through)
}

// previousCatchup returns the answers_through of the actor's latest catchup
// event, and false when it never caught up.
func (s Store) previousCatchup(ctx context.Context, handle string) (int64, bool, error) {
	d := s.Cursors.Store.Dialect()
	var data string
	err := s.Cursors.Store.Reader().QueryRowContext(ctx, d.Rebind(
		`SELECT e.data FROM events e JOIN actors a ON a.id = e.actor_id WHERE e.type = ? AND a.handle = ? ORDER BY e.n DESC LIMIT 1`),
		EventCatchup, handle).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read previous catch-up: %w", err)
	}
	var marker struct {
		Through int64 `json:"answers_through"`
	}
	if err := json.Unmarshal([]byte(data), &marker); err != nil {
		return 0, false, fmt.Errorf("read previous catch-up: %w", err)
	}
	return marker.Through, true, nil
}

// record moves the notes cursor to the last note of the catch-up and appends
// its catchup event, in one transaction.
func (s Store) record(ctx context.Context, actor audit.Actor, principal string, r CatchupResult, through int64) error {
	tx, err := s.Cursors.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := s.recordIn(ctx, tx, actor, principal, r, through); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s Store) recordIn(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, r CatchupResult, through int64) error {
	if len(r.Notes) > 0 {
		if err := s.Cursors.AckIn(ctx, tx, actor, cursors.StreamNotes, r.Notes[len(r.Notes)-1].ID); err != nil {
			return err
		}
	}
	return audit.Append(ctx, tx, s.Cursors.Store.Dialect(), actor, s.now(), audit.Event{
		Principal: principal, Type: EventCatchup,
		Summary: fmt.Sprintf("catch-up: %d notes, %d answers", len(r.Notes), len(r.Answers)),
		Data:    map[string]any{"notes": len(r.Notes), "answers": len(r.Answers), "answers_through": through},
	})
}
