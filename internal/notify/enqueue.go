package notify

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

var columns = []string{"id", "kind", "event_id", "payload", "state", "attempts", "next_attempt_at", "created_at"}

// Enqueue adds a pending notification of kind for the event eventID, with the payload of BuildPayload, in tx, when
// alerts are on and BOARD_NOTIFY_ON names the kind. It writes nothing otherwise.
// The outbox is unique on (event, kind), so an event never enqueues a kind twice.
func Enqueue(ctx context.Context, tx *sql.Tx, st *store.Store, eventID, kind string, now time.Time) error {
	if !st.Notifies(kind) {
		return nil
	}
	id, err := domain.NewULID(now)
	if err != nil {
		return err
	}
	payload, err := BuildPayload(ctx, tx, st, eventID, kind, st.PublicURL())
	if err != nil {
		return err
	}
	at := store.EncodeTime(now)
	_, err = tx.ExecContext(ctx, st.Dialect().Rebind(st.Dialect().Insert("notifications", columns)),
		id, kind, eventID, string(payload), "pending", 0, at, at)
	if err != nil {
		return fmt.Errorf("enqueue %s notification: %w", kind, err)
	}
	return nil
}

// EnqueueStranded enqueues the stranded notification of the lease_expired event
// eventID when the lease leaseID is the newest on its task, by started_at and
// then id. A lease with a later one on the same task enqueues nothing.
func EnqueueStranded(ctx context.Context, tx *sql.Tx, st *store.Store, eventID, leaseID string, now time.Time) error {
	if !st.Notifies(store.NotifyStranded) {
		return nil
	}
	var later int
	err := tx.QueryRowContext(ctx, st.Dialect().Rebind(`SELECT COUNT(*) FROM leases o JOIN leases l ON l.task_id = o.task_id
		WHERE l.id = ? AND (o.started_at > l.started_at OR (o.started_at = l.started_at AND o.id > l.id))`), leaseID).Scan(&later)
	if err != nil {
		return fmt.Errorf("find the newest lease: %w", err)
	}
	if later > 0 {
		return nil
	}
	return Enqueue(ctx, tx, st, eventID, store.NotifyStranded, now)
}
