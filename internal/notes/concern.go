package notes

import (
	"context"
	"fmt"
	"strings"
)

// Concern selects the notes that concern an actor (SPEC 6.5): a note on one of
// Tasks (refs or ULIDs), on one of Resources, one mentioning Handle, or a
// broadcast, among those after the note After (a ULID; empty for all).
type Concern struct {
	Handle    string
	Tasks     []string
	Resources []string
	After     string
}

// Concerning returns the first limit notes that concern c, oldest first and
// without bodies, and how many more there are after them. A task that names
// nothing is ErrNotFound.
func (s Store) Concerning(ctx context.Context, c Concern, limit int) ([]Note, int, error) {
	f, err := s.concernFilter(ctx, c)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.rows(ctx, f, limit, false)
	if err != nil {
		return nil, 0, err
	}
	query := `SELECT COUNT(*) FROM notes k`
	if len(f.conds) > 0 {
		query += ` WHERE ` + strings.Join(f.conds, ` AND `)
	}
	var total int
	if err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(query), f.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notes: %w", err)
	}
	return items, total - len(items), nil
}

// concernFilter is the conditions of a Concern: its four reasons are OR-ed and
// the whole is AND-ed with the after cursor.
func (s Store) concernFilter(ctx context.Context, c Concern) (*filter, error) {
	reasons := &filter{}
	reasons.add(broadcastSQL)
	if err := reasons.mention(c.Handle); err != nil {
		return nil, err
	}
	reasons.resources(c.Resources)
	ids, err := s.taskIDs(ctx, c.Tasks)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		reasons.add(`k.task_id IN (`+marks+`)`, args...)
	}
	f := &filter{}
	f.add(`(`+strings.Join(reasons.conds, ` OR `)+`)`, reasons.args...)
	if c.After != "" {
		if err := after(f, c.After); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (s Store) taskIDs(ctx context.Context, refs []string) ([]string, error) {
	ids := make([]string, len(refs))
	for i, ref := range refs {
		t, err := s.findTask(ctx, s.Store.Reader(), ref)
		if err != nil {
			return nil, err
		}
		ids[i] = t.ID
	}
	return ids, nil
}
