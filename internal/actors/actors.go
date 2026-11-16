package actors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/store"
)

// ErrNegativeVersion is the error for a protocol version below 0.
var ErrNegativeVersion = errors.New("protocol version must be 0 or higher")

// Store reads and writes the actors of one database.
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

// RecordProtocolSeen upserts the actor and raises its protocol_seen to version
// when that is higher than what is recorded, since the column holds the highest
// version the actor has read. It returns the value the column holds after.
func (s Store) RecordProtocolSeen(ctx context.Context, actor audit.Actor, version int) (int, error) {
	if version < 0 {
		return 0, ErrNegativeVersion
	}
	d := s.Store.Dialect()
	tx, err := s.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	seen, err := raise(ctx, tx, d, actor, version, s.now())
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return seen, nil
}

// raise does the writes of RecordProtocolSeen in tx.
func raise(ctx context.Context, tx *sql.Tx, d store.Dialect, actor audit.Actor, version int, now time.Time) (int, error) {
	id, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(`UPDATE actors SET protocol_seen = ? WHERE id = ? AND (protocol_seen IS NULL OR protocol_seen < ?)`),
		version, id, version)
	if err != nil {
		return 0, fmt.Errorf("record protocol seen: %w", err)
	}
	var seen int
	if err := tx.QueryRowContext(ctx, d.Rebind(`SELECT protocol_seen FROM actors WHERE id = ?`), id).Scan(&seen); err != nil {
		return 0, fmt.Errorf("record protocol seen: %w", err)
	}
	return seen, nil
}
