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
	err := s.inTx(ctx, func(tx *sql.Tx) error {
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
		if before.State != domain.AskOpen || before.Mode != domain.AskModePoll || s.Waiting(before) {
			return nil
		}
		return s.appendPolling(ctx, tx, before)
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("record the poll: %w", err)
	}
	return store.DecodeTime(at), nil
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
