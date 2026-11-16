package leases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/notify"
	"github.com/n-orlov/village-hall/internal/store"
)

// systemPrincipal is the principal of an event the board itself writes.
const systemPrincipal = "system:expiry"

// due is a live lease past its expiry.
type due struct {
	ID, ActorID, TaskID, InitiativeID, Ref, Task string
	ExpiresAt                                    time.Time
}

// ExpireDue marks every live lease whose expires_at is before now as expired and
// appends one lease_expired event for each, in one transaction, and returns how
// many it expired. The update is conditional on the lease still being live, so
// a second caller (another server on the same database) finds nothing to do and
// writes no event.
func (s Store) ExpireDue(ctx context.Context) (int, error) { return s.expireAt(ctx, s.now()) }

// expireAt is ExpireDue judged at the instant now, so a read can expire and
// then list at one and the same instant.
//
// A read comes here on every check-in, so it looks for a due lease on a reader
// first and takes the writer connection only when one is there (SPEC 14: reads
// never take the SQLite writer connection). A lease that falls due between the
// look and the listing is expired by the next call; the listing itself drops
// leases past their expiry.
func (s Store) expireAt(ctx context.Context, now time.Time) (int, error) {
	pending, err := s.anyDue(ctx, now)
	if err != nil || !pending {
		return 0, err
	}
	var n int
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		n, err = s.expireDue(ctx, tx, now)
		return err
	})
	return n, err
}

// anyDue reports whether a live lease expired before now, without the writer.
func (s Store) anyDue(ctx context.Context, now time.Time) (bool, error) {
	var one int
	err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(
		`SELECT 1 FROM leases WHERE state = 'live' AND expires_at < ? LIMIT 1`), store.EncodeBound(now)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look for expired leases: %w", err)
	}
	return true, nil
}

func (s Store) expireDue(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	list, err := s.dueAt(ctx, tx, now)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range list {
		res, err := tx.ExecContext(ctx, s.Store.Dialect().Rebind(`UPDATE leases SET state = 'expired' WHERE id = ? AND state = 'live' AND expires_at < ?`),
			l.ID, store.EncodeBound(now))
		if err != nil {
			return n, fmt.Errorf("expire lease: %w", err)
		}
		if won, _ := res.RowsAffected(); won != 1 {
			continue
		}
		if err := s.appendExpired(ctx, tx, l, now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// dueAt lists the live leases that expired before now, oldest first.
func (s Store) dueAt(ctx context.Context, tx *sql.Tx, now time.Time) ([]due, error) {
	rows, err := tx.QueryContext(ctx, s.Store.Dialect().Rebind(`SELECT l.id, l.n, l.actor_id, l.task_id, t.initiative_id, i.slug, t.seq, l.expires_at
		FROM leases l JOIN tasks t ON t.id = l.task_id JOIN initiatives i ON i.id = t.initiative_id
		WHERE l.state = 'live' AND l.expires_at < ? ORDER BY l.expires_at, l.n`), store.EncodeBound(now))
	if err != nil {
		return nil, fmt.Errorf("read expired leases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []due
	for rows.Next() {
		var l due
		var n int64
		var slug string
		var seq int
		var exp int64
		if err := rows.Scan(&l.ID, &n, &l.ActorID, &l.TaskID, &l.InitiativeID, &slug, &seq, &exp); err != nil {
			return nil, fmt.Errorf("read expired leases: %w", err)
		}
		l.Ref, l.Task, l.ExpiresAt = domain.FormatLeaseRef(n), domain.FormatTaskRef(slug, seq), store.DecodeTime(exp)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s Store) appendExpired(ctx context.Context, tx *sql.Tx, l due, now time.Time) error {
	eventID, err := audit.AppendAsID(ctx, tx, s.Store.Dialect(), l.ActorID, now, audit.Event{
		Principal: systemPrincipal, Type: EventExpired, InitiativeID: l.InitiativeID, TaskID: l.TaskID, RefID: l.ID,
		Summary: l.Ref + " expired on " + l.Task,
		Data:    map[string]any{"ref": l.Ref, "task": l.Task, "expired_at": l.ExpiresAt.Format(time.RFC3339Nano)},
	})
	if err != nil {
		return err
	}
	return notify.EnqueueStranded(ctx, tx, s.Store, eventID, l.ID, now)
}
