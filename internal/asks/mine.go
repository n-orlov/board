package asks

import (
	"context"
	"time"

	"github.com/n-orlov/board/internal/store"
)

// Mine lists the asks the actor with this handle raised that its check-in shows
// (SPEC 6.5): every open one, and every answered one that was answered at or
// after answeredSince, with the answer. Withdrawn asks are left out. They are
// ordered by id, oldest first, and the list is empty, never nil.
func (s Store) Mine(ctx context.Context, handle string, answeredSince time.Time) ([]Ask, error) {
	f := &filter{}
	f.add(`a.handle = ?`, handle)
	f.add(`(k.state = 'open' OR (k.state = 'answered' AND k.answered_at >= ?))`, store.EncodeBound(answeredSince))
	return s.rows(ctx, f, 1000)
}
