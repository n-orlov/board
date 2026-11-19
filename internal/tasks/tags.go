package tasks

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
)

// tagQuerier is the part of *sql.DB, *sql.Tx and store.Querier that reads rows.
type tagQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// normalizeTags applies the initiative tag rule (SPEC 4.2) to a task's own tags:
// trimmed, lowercased, deduplicated and sorted, at most 20, each matching
// domain.TagPattern. The result is never nil.
func normalizeTags(tags []string) ([]string, error) {
	out, err := domain.NormalizeTags(tags)
	if err != nil {
		return nil, InvalidError{Msg: err.Error(), Next: initiatives.NextTags}
	}
	return out, nil
}

// insertTags writes the task's tags in tx.
func (s Store) insertTags(ctx context.Context, tx *sql.Tx, taskID string, tags []string) error {
	q := s.Store.Dialect().Rebind(`INSERT INTO task_tags (task_id, tag) VALUES (?, ?)`)
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, q, taskID, tag); err != nil {
			return fmt.Errorf("write task tag: %w", err)
		}
	}
	return nil
}

// loadTags sets Tags on every task of list from task_tags, sorted, and to an
// empty list for a task that has none.
func (s Store) loadTags(ctx context.Context, q tagQuerier, list []Task) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]string, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	byTask, err := s.tagsByTask(ctx, q, ids)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].Tags = append([]string{}, byTask[list[i].ID]...)
	}
	return nil
}

// tagsByTask reads the sorted tags of the tasks with these ids.
func (s Store) tagsByTask(ctx context.Context, q tagQuerier, ids []string) (map[string][]string, error) {
	rows, err := q.QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT task_id, tag FROM task_tags WHERE task_id IN (`+marks(len(ids))+`) ORDER BY task_id, tag`), anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("read task tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byTask := map[string][]string{}
	for rows.Next() {
		var id, tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, fmt.Errorf("read task tags: %w", err)
		}
		byTask[id] = append(byTask[id], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read task tags: %w", err)
	}
	return byTask, nil
}
