package tasks

import (
	"context"
	"fmt"

	"github.com/n-orlov/village-hall/internal/domain"
)

// Link is a task on the other end of a dependency: its ref, title and status.
type Link struct {
	Ref    string
	Title  string
	Status string
}

// Links are the two lists a task read carries: the tasks it is blocked by and
// the tasks it blocks, each ordered by the task's id (creation order).
type Links struct {
	Deps     []Link
	Blocking []Link
}

// Links reads the dependencies of the task t names: Deps are the tasks t is
// blocked by (open or not), Blocking the tasks that name t as a blocker. Both
// lists are empty, never nil.
func (s Store) Links(ctx context.Context, t Task) (Links, error) {
	deps, err := s.linked(ctx, t.ID, `td.blocked_by_task_id`, `td.task_id`)
	if err != nil {
		return Links{}, err
	}
	blocking, err := s.linked(ctx, t.ID, `td.task_id`, `td.blocked_by_task_id`)
	if err != nil {
		return Links{}, err
	}
	return Links{Deps: deps, Blocking: blocking}, nil
}

// linked reads the tasks joined through column other of the dependencies whose
// column self is id. The two column names are constants of this package.
func (s Store) linked(ctx context.Context, id, other, self string) ([]Link, error) {
	rows, err := s.Store.ReaderFor(ctx).QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT i.slug, o.seq, o.title, o.status FROM task_deps td JOIN tasks o ON o.id = `+other+`
		JOIN initiatives i ON i.id = o.initiative_id WHERE `+self+` = ? ORDER BY o.id`), id)
	if err != nil {
		return nil, fmt.Errorf("read dependencies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Link{}
	for rows.Next() {
		var (
			l    Link
			slug string
			seq  int
		)
		if err := rows.Scan(&slug, &seq, &l.Title, &l.Status); err != nil {
			return nil, fmt.Errorf("read dependencies: %w", err)
		}
		l.Ref = domain.FormatTaskRef(slug, seq)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dependencies: %w", err)
	}
	return out, nil
}
