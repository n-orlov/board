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
	// TaskTitle, Intent, Handle and WayBack are what the stranded alert says;
	// ActorOrigin is the holder's own origin as an event of it records; Newest
	// says no later lease exists on the task.
	TaskTitle, Intent, Handle, WayBack, ActorOrigin string
	Newest                                          bool
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
	err := s.Store.ReaderFor(ctx).QueryRowContext(ctx, s.Store.Dialect().Rebind(
		`SELECT 1 FROM leases WHERE state = 'live' AND expires_at < ? LIMIT 1`), store.EncodeBound(now)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look for expired leases: %w", err)
	}
	return true, nil
}

// expireDue expires every due lease and records it, in a number of statements that
// does not depend on how many are due: one read that locks them, one update, one
// INSERT of the events and, with stranded alerts on, one of the notifications,
// run even when no expired lease is the newest on its task. The read holds the rows, so the update finds the very leases the read
// listed; a count that differs means another writer got in between, and the
// transaction rolls back rather than expire a lease with no event.
func (s Store) expireDue(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	list, err := s.dueAt(ctx, tx, now)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, s.Store.Dialect().Rebind(`UPDATE leases SET state = 'expired' WHERE state = 'live' AND expires_at < ?`),
		store.EncodeBound(now))
	if err != nil {
		return 0, fmt.Errorf("expire leases: %w", err)
	}
	if err := wonAll(res, len(list)); err != nil {
		return 0, err
	}
	if err := s.appendExpired(ctx, tx, list, now); err != nil {
		return 0, err
	}
	return len(list), nil
}

// wonAll is nil when an update touched exactly the want rows the caller listed.
func wonAll(res sql.Result, want int) error {
	got, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("expire leases: %w", err)
	}
	if got != int64(want) {
		return fmt.Errorf("expire leases: %d leases listed but %d expired, so another writer changed them", want, got)
	}
	return nil
}

// dueAt lists the live leases that expired before now, oldest first, with what
// their events and stranded alerts say, and locks them where the database can.
func (s Store) dueAt(ctx context.Context, tx *sql.Tx, now time.Time) ([]due, error) {
	rows, err := tx.QueryContext(ctx, s.Store.Dialect().Rebind(`SELECT l.id, l.n, l.actor_id, l.task_id, t.initiative_id, i.slug, t.seq, t.title,
			l.intent, l.expires_at, a.handle, a.way_back, CASE WHEN a.kind = 'operator' THEN a.id ELSE a.origin END,
			CASE WHEN EXISTS (SELECT 1 FROM leases o WHERE o.task_id = l.task_id
				AND (o.started_at > l.started_at OR (o.started_at = l.started_at AND o.id > l.id))) THEN 0 ELSE 1 END
		FROM leases l JOIN tasks t ON t.id = l.task_id JOIN initiatives i ON i.id = t.initiative_id JOIN actors a ON a.id = l.actor_id
		WHERE l.state = 'live' AND l.expires_at < ? ORDER BY l.expires_at, l.n`+s.Store.Dialect().LockRow("l")), store.EncodeBound(now))
	if err != nil {
		return nil, fmt.Errorf("read expired leases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []due
	for rows.Next() {
		var l due
		var n int64
		var slug string
		var seq, newest int
		var exp int64
		var wayBack, origin sql.NullString
		if err := rows.Scan(&l.ID, &n, &l.ActorID, &l.TaskID, &l.InitiativeID, &slug, &seq, &l.TaskTitle,
			&l.Intent, &exp, &l.Handle, &wayBack, &origin, &newest); err != nil {
			return nil, fmt.Errorf("read expired leases: %w", err)
		}
		l.Ref, l.Task, l.ExpiresAt = domain.FormatLeaseRef(n), domain.FormatTaskRef(slug, seq), store.DecodeTime(exp)
		l.WayBack, l.ActorOrigin, l.Newest = wayBack.String, origin.String, newest == 1
		out = append(out, l)
	}
	return out, rows.Err()
}

// appendExpired records a lease_expired event for each lease of list, then the
// stranded alerts of those that are the newest on their task.
func (s Store) appendExpired(ctx context.Context, tx *sql.Tx, list []due, now time.Time) error {
	events := make([]audit.Written, len(list))
	for i, l := range list {
		events[i] = audit.Written{ActorID: l.ActorID, ActorOrigin: l.ActorOrigin, Event: audit.Event{
			Principal: systemPrincipal, Type: EventExpired, InitiativeID: l.InitiativeID, TaskID: l.TaskID, RefID: l.ID,
			Summary: l.Ref + " expired on " + l.Task,
			Data:    map[string]any{"ref": l.Ref, "task": l.Task, "expired_at": l.ExpiresAt.Format(time.RFC3339Nano)},
		}}
	}
	ids, err := audit.AppendBatch(ctx, tx, s.Store.Dialect(), now, events)
	if err != nil {
		return err
	}
	alerts := make([]notify.Stranded, len(list))
	for i, l := range list {
		alerts[i] = notify.Stranded{EventID: ids[i], Newest: l.Newest, Holder: notify.Actor{Handle: l.Handle, WayBack: l.WayBack},
			LeaseRef: l.Ref, Intent: l.Intent, ExpiredAt: l.ExpiresAt, TaskRef: l.Task, TaskTitle: l.TaskTitle}
	}
	return notify.EnqueueStrandedBatch(ctx, tx, s.Store, alerts, now)
}
