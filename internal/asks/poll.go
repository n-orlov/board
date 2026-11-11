package asks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// EventPolling is the event a poll mark appends when it turns the asker of an
// open poll ask from not waiting to waiting: the change of the displayed
// asker_waiting reaches the change feed, so every open view, on every replica
// sharing the database, refreshes. A mark that renews a poll already counted as
// waiting writes none.
const EventPolling = "ask_polling"

// pollPrincipal is the principal of an ask_polling event: the board writes it
// for the asker's poll, as it does a lease's expiry.
const pollPrincipal = "system:poll"

// Touch records that a long poll on the ask with this ULID held or returned
// now: poll_seen_at becomes the clock. It is stored, not remembered in memory,
// so asker_waiting is right on every replica. It appends an ask_polling event
// when that makes an open poll ask's asker waiting (MarkPoll), and it is a
// no-op for an id that names no ask.
func (s Store) Touch(ctx context.Context, id string) error {
	_, err := s.MarkPoll(ctx, id)
	return err
}

// MarkPoll is Touch, returning the poll_seen_at it stored. The mark and the
// ask_polling event of a change to waiting are one transaction on the writer.
func (s Store) MarkPoll(ctx context.Context, id string) (time.Time, error) {
	at := store.EncodeTime(s.now())
	err := s.inTx(ctx, func(tx *sql.Tx) error { return s.markPollTx(ctx, tx, id, at) })
	if err != nil {
		return time.Time{}, fmt.Errorf("record the poll: %w", err)
	}
	return store.DecodeTime(at), nil
}

// markPollTx stores the poll mark at on the ask with this ULID inside tx, and
// appends the ask_polling event when that makes the asker waiting. An id that
// names no ask is not an error.
func (s Store) markPollTx(ctx context.Context, tx *sql.Tx, id string, at int64) error {
	before, err := s.find(ctx, tx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	q := s.Store.Dialect().Rebind(`UPDATE asks SET poll_seen_at = ? WHERE id = ?`)
	if _, err := tx.ExecContext(ctx, q, at, before.ID); err != nil {
		return fmt.Errorf("record the poll: %w", err)
	}
	if !s.startsWaiting(before, store.DecodeTime(at)) {
		return nil
	}
	return s.appendPolling(ctx, tx, before)
}

// startsWaiting reports whether a poll mark makes k's asker waiting: k is an
// open poll ask whose asker is not yet counted as waiting as of the instant the
// mark stores. The same instant decides and is stored: a second clock read
// could find the previous mark lapsed that the stored one never saw, and leave
// a mark older than the event that announced it.
func (s Store) startsWaiting(k Ask, at time.Time) bool {
	return k.State == domain.AskOpen && k.Mode == domain.AskModePoll && !s.waitingAt(k, at)
}

// appendPolling appends the ask_polling event of k, made by its asker.
func (s Store) appendPolling(ctx context.Context, tx *sql.Tx, k Ask) error {
	data := map[string]any{"ref": k.Ref, "waiting": true}
	if k.Task != "" {
		data["task"] = k.Task
	}
	if k.Initiative != "" {
		data["initiative"] = k.Initiative
	}
	return audit.AppendAs(ctx, tx, s.Store.Dialect(), k.ActorID, s.now(), audit.Event{
		Principal: pollPrincipal, Type: EventPolling, InitiativeID: k.InitiativeID, TaskID: k.TaskID, RefID: k.ID,
		Summary: eventSummary(k, "asker waiting", ""), Data: data,
	})
}
