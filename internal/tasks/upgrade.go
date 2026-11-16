package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

const (
	// upgradePrincipal is the principal of an event the start-up upgrade writes.
	upgradePrincipal = "system:upgrade"
	// upgradeReason is the data.reason of the task_reopened that upgrade appends.
	upgradeReason = "live lease at upgrade"
)

// leasedTask is a done task that holds a live lease, with the oldest such lease.
type leasedTask struct {
	id, initiativeID, ref string
	holderID, leaseRef    string
}

// ReopenLeased reopens every done task that holds a live lease (a lease in
// state live whose expires_at is not before now), in one transaction. Each
// gets `task_reopened` made by the holder of its oldest live lease, principal
// `system:upgrade`, data.reason `live lease at upgrade`. It returns how many it
// reopened. The update is conditional on the task still being done, so a second
// caller, or another server starting on the same database together with this
// one, finds nothing to do and writes no event.
func (s Store) ReopenLeased(ctx context.Context) (int, error) {
	n := 0
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		list, err := s.leasedDone(ctx, tx, now)
		if err != nil {
			return err
		}
		n = 0
		for _, t := range list {
			ok, err := s.reopenLeased(ctx, tx, t, now)
			if err != nil {
				return err
			}
			if ok {
				n++
			}
		}
		return nil
	})
	return n, err
}

// leasedDone lists the done tasks with a live lease at now, in id order, each
// with the holder of its oldest live lease.
func (s Store) leasedDone(ctx context.Context, tx *sql.Tx, now time.Time) ([]leasedTask, error) {
	rows, err := tx.QueryContext(ctx, s.Store.Dialect().Rebind(`SELECT t.id, t.initiative_id, i.slug, t.seq, l.actor_id, l.n
		FROM tasks t JOIN initiatives i ON i.id = t.initiative_id JOIN leases l ON l.task_id = t.id
		WHERE t.status = 'done' AND l.state = 'live' AND l.expires_at >= ? ORDER BY t.id, l.n`), store.EncodeBound(now))
	if err != nil {
		return nil, fmt.Errorf("read done tasks with a live lease: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []leasedTask
	for rows.Next() {
		var t leasedTask
		var slug string
		var seq int
		var n int64
		if err := rows.Scan(&t.id, &t.initiativeID, &slug, &seq, &t.holderID, &n); err != nil {
			return nil, fmt.Errorf("read done tasks with a live lease: %w", err)
		}
		if len(out) > 0 && out[len(out)-1].id == t.id {
			continue
		}
		t.ref, t.leaseRef = domain.FormatTaskRef(slug, seq), domain.FormatLeaseRef(n)
		out = append(out, t)
	}
	return out, rows.Err()
}

// reopenLeased reopens one task and appends its event, and reports whether it
// won: false when the task was no longer done.
func (s Store) reopenLeased(ctx context.Context, tx *sql.Tx, t leasedTask, now time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, s.Store.Dialect().Rebind(`UPDATE tasks SET status = 'open', resolution = NULL, done_at = NULL, done_by = NULL, closed_at = NULL, closed_by = NULL, updated_at = ?
		WHERE id = ? AND status = 'done'`), store.EncodeTime(now), t.id)
	if err != nil {
		return false, fmt.Errorf("reopen task: %w", err)
	}
	if won, _ := res.RowsAffected(); won != 1 {
		return false, nil
	}
	return true, audit.AppendAs(ctx, tx, s.Store.Dialect(), t.holderID, now, audit.Event{
		Principal: upgradePrincipal, Type: EventReopened, InitiativeID: t.initiativeID, TaskID: t.id, RefID: t.id,
		Summary: "task " + t.ref + " " + eventVerb[EventReopened],
		Data:    map[string]any{"ref": t.ref, "from": "done", "reason": upgradeReason, "lease": t.leaseRef},
	})
}
