package leases

import (
	"context"
	"fmt"
	"strings"

	"github.com/n-orlov/village-hall/internal/store"
)

// LiveOf lists the live leases on the task whose id is taskID, oldest first: those
// in state live whose expires_at is not before now. It first expires the due
// leases (SPEC §5.1), so a lease whose time has run out is marked expired, with
// its one lease_expired event, and left out; the list is empty, never nil, when
// the task has none.
func (s Store) LiveOf(ctx context.Context, taskID string) ([]Lease, error) {
	now := s.now()
	if _, err := s.expireAt(ctx, now); err != nil {
		return nil, err
	}
	return s.readLeases(ctx, selectLease+` WHERE l.task_id = ? AND l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`,
		[]any{taskID, store.EncodeBound(now)})
}

// LiveFor lists the live leases on each of the tasks whose ids are taskIDs, in
// one map by task id, oldest first. A task with none has no entry. Unlike LiveOf
// it does not expire the due leases first: it leaves out the ones whose time has
// run out, and a caller that wants them marked expired calls ExpireDue itself.
func (s Store) LiveFor(ctx context.Context, taskIDs []string) (map[string][]Lease, error) {
	out := map[string][]Lease{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	args := []any{store.EncodeBound(s.now())}
	for _, id := range taskIDs {
		args = append(args, id)
	}
	list, err := s.readLeases(ctx, selectLease+` WHERE l.state = 'live' AND l.expires_at >= ? AND l.task_id IN (`+
		strings.TrimSuffix(strings.Repeat("?, ", len(taskIDs)), ", ")+`) ORDER BY l.n`, args)
	if err != nil {
		return nil, err
	}
	for _, l := range list {
		out[l.TaskID] = append(out[l.TaskID], l)
	}
	return out, nil
}

// LiveCountsFor counts the live leases on the tasks of each initiative whose id
// is in initiativeIDs, in one map by initiative id. An initiative with none has
// no entry. Like LiveFor it leaves out the leases whose time has run out without
// expiring them; a caller that wants them marked expired calls ExpireDue first.
func (s Store) LiveCountsFor(ctx context.Context, initiativeIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(initiativeIDs) == 0 {
		return out, nil
	}
	args := []any{store.EncodeBound(s.now())}
	for _, id := range initiativeIDs {
		args = append(args, id)
	}
	rows, err := s.Store.ReaderFor(ctx).QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT t.initiative_id, COUNT(*) FROM leases l JOIN tasks t ON t.id = l.task_id
		 WHERE l.state = 'live' AND l.expires_at >= ? AND t.initiative_id IN (`+
			strings.TrimSuffix(strings.Repeat("?, ", len(initiativeIDs)), ", ")+`) GROUP BY t.initiative_id`), args...)
	if err != nil {
		return nil, fmt.Errorf("count live leases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("count live leases: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}
