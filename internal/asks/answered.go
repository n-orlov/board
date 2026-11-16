package asks

import (
	"context"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

// answeredIn selects the asks the actor with this handle raised whose
// ask_answered event satisfies cond (a condition on the events row e). They are
// ordered by id, oldest first, and the list is empty, never nil.
func (s Store) answeredIn(ctx context.Context, handle, cond string, args ...any) ([]Ask, error) {
	f := &filter{}
	f.add(`a.handle = ?`, handle)
	f.add(`k.state = 'answered'`)
	f.add(`k.id IN (SELECT e.ref_id FROM events e WHERE e.type = ? AND `+cond+`)`, append([]any{EventAnswered}, args...)...)
	return s.rows(ctx, f, 1000)
}

// AnsweredBetween lists the asks the actor raised that were answered by an event
// after the event number after and up to the number upTo, which is how a
// catch-up delivers each answer once.
func (s Store) AnsweredBetween(ctx context.Context, handle string, after, upTo int64) ([]Ask, error) {
	return s.answeredIn(ctx, handle, `e.n > ? AND e.n <= ?`, after, upTo)
}

// AnsweredSince lists the asks the actor raised that were answered at or after
// since and by an event up to the number upTo.
func (s Store) AnsweredSince(ctx context.Context, handle string, since time.Time, upTo int64) ([]Ask, error) {
	return s.answeredIn(ctx, handle, `e.at >= ? AND e.n <= ?`, store.EncodeBound(since), upTo)
}
