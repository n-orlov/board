package leases

import (
	"context"
	"strings"

	"github.com/n-orlov/board/internal/store"
)

// LiveOf lists the live leases on the task whose id is taskID, oldest first: those
// in state live whose expires_at is not before now. It first expires the due
// leases (SPEC §5.1), so a lease whose time has run out is marked expired, with
// its one lease_expired event, and left out; the list is empty, never nil, when
// the task has none.
func (s Store) LiveOf(ctx context.Context, taskID string) ([]Lease, error) {
	if _, err := s.ExpireDue(ctx); err != nil {
		return nil, err
	}
	return s.readLeases(ctx, selectLease+` WHERE l.task_id = ? AND l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`,
		[]any{taskID, store.EncodeTime(s.now())})
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
	args := []any{store.EncodeTime(s.now())}
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
