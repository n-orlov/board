package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/n-orlov/village-hall/internal/store"
)

type originContextKey struct{}

// WithOrigin returns ctx carrying the handle of the operator behind the request
// (SPEC 3.1): a session's own operator, or the owner of the key it came through.
// An empty handle says the request has no known operator, so what it writes
// carries a null origin. A ctx that never passed through WithOrigin belongs to
// no request (a lease expiring on its own, say).
func WithOrigin(ctx context.Context, handle string) context.Context {
	return context.WithValue(ctx, originContextKey{}, handle)
}

// OriginFrom returns the handle WithOrigin recorded, and whether it recorded one.
func OriginFrom(ctx context.Context) (string, bool) {
	h, ok := ctx.Value(originContextKey{}).(string)
	return h, ok
}

// setOrigin records the origin of the agent row actorID from its latest write:
// the operator the handle names, or null when the handle is empty (a key with
// no owner) or names no operator, so a write through an ownerless key clears
// the origin an earlier key left. A nil handle (a write outside any request,
// or the actor a write is made on behalf of) and an operator row leave the row
// as it is.
func setOrigin(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string, handle *string) error {
	if handle == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, d.Rebind(`UPDATE actors SET origin = (SELECT o.id FROM actors o WHERE o.handle = ? AND o.kind = 'operator')
		WHERE id = ? AND kind = 'agent'`),
		*handle, actorID)
	if err != nil {
		return fmt.Errorf("record actor origin: %w", err)
	}
	return nil
}

// OperatorID returns the id of the operator actor with the handle, or "" when
// no operator has it.
func OperatorID(ctx context.Context, tx *sql.Tx, d store.Dialect, handle string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, d.Rebind(`SELECT id FROM actors WHERE handle = ? AND kind = 'operator'`), handle).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// eventOrigin is the origin column of an event the actor row actorID writes: the
// operator the request carries, or null when it carries none. A write outside
// any request takes the actor's own: an operator row is its own origin, an
// agent row's origin is the one its latest write left.
func eventOrigin(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string) (any, error) {
	if handle, ok := OriginFrom(ctx); ok {
		if handle == "" {
			return nil, nil
		}
		return idOrNil(OperatorID(ctx, tx, d, handle))
	}
	var origin sql.NullString
	err := tx.QueryRowContext(ctx, d.Rebind(`SELECT CASE WHEN kind = 'operator' THEN id ELSE origin END FROM actors WHERE id = ?`), actorID).Scan(&origin)
	if err != nil || !origin.Valid {
		return nil, err
	}
	return origin.String, nil
}

func idOrNil(id string, err error) (any, error) {
	if err != nil || id == "" {
		return nil, err
	}
	return id, nil
}
