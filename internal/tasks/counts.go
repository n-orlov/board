package tasks

import (
	"context"
	"fmt"
	"strings"
)

// Counts is how many tasks of one initiative are open, done and closed.
type Counts struct {
	Open, Done, Closed int
}

// CountsFor counts the tasks of each initiative whose id is in initiativeIDs, by
// status. An initiative with no tasks maps to zero counts.
func (s Store) CountsFor(ctx context.Context, initiativeIDs []string) (map[string]Counts, error) {
	out := make(map[string]Counts, len(initiativeIDs))
	if len(initiativeIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(initiativeIDs))
	for i, id := range initiativeIDs {
		args[i] = id
		out[id] = Counts{}
	}
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT initiative_id, status, COUNT(*) FROM tasks WHERE initiative_id IN (`+
			strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ")+`) GROUP BY initiative_id, status`), args...)
	if err != nil {
		return nil, fmt.Errorf("count tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, status string
		var n int
		if err := rows.Scan(&id, &status, &n); err != nil {
			return nil, fmt.Errorf("count tasks: %w", err)
		}
		out[id] = out[id].with(status, n)
	}
	return out, rows.Err()
}

// with is c with n more tasks of the status.
func (c Counts) with(status string, n int) Counts {
	switch status {
	case StatusOpen:
		c.Open += n
	case StatusDone:
		c.Done += n
	case StatusClosed:
		c.Closed += n
	}
	return c
}
