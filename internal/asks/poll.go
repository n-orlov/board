package asks

import (
	"context"
	"fmt"
	"time"

	"github.com/n-orlov/board/internal/store"
)

// Touch records that a long poll on the ask with this ULID held or returned
// now: poll_seen_at becomes the clock. It is stored, not remembered in memory,
// so asker_waiting is right on every replica. It writes no event, and it is a
// no-op for an id that names no ask.
func (s Store) Touch(ctx context.Context, id string) error {
	_, err := s.MarkPoll(ctx, id)
	return err
}

// MarkPoll is Touch, returning the poll_seen_at it stored.
func (s Store) MarkPoll(ctx context.Context, id string) (time.Time, error) {
	ms := store.EncodeTime(s.now())
	q := s.Store.Dialect().Rebind(`UPDATE asks SET poll_seen_at = ? WHERE id = ?`)
	if _, err := s.Store.Writer().ExecContext(ctx, q, ms, id); err != nil {
		return time.Time{}, fmt.Errorf("record the poll: %w", err)
	}
	return store.DecodeTime(ms), nil
}
