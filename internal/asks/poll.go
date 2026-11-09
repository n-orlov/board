package asks

import (
	"context"
	"fmt"

	"github.com/n-orlov/board/internal/store"
)

// Touch records that a long poll on the ask with this ULID held or returned
// now: poll_seen_at becomes the clock. It is stored, not remembered in memory,
// so asker_waiting is right on every replica. It writes no event, and it is a
// no-op for an id that names no ask.
func (s Store) Touch(ctx context.Context, id string) error {
	q := s.Store.Dialect().Rebind(`UPDATE asks SET poll_seen_at = ? WHERE id = ?`)
	if _, err := s.Store.Writer().ExecContext(ctx, q, store.EncodeTime(s.now()), id); err != nil {
		return fmt.Errorf("record the poll: %w", err)
	}
	return nil
}
