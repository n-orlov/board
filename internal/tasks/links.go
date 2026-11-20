package tasks

import (
	"context"
	"fmt"

	"github.com/n-orlov/village-hall/internal/domain"
)

// Link is a task on the other end of a dependency: its ref, title, status, its
// own tags and every mark, as a task read gives them.
type Link struct {
	Ref      string
	Title    string
	Status   string
	Tags     []string
	TagMarks []TagMark
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
// column self is id, each with its tags. The two column names are constants of
// this package.
func (s Store) linked(ctx context.Context, id, other, self string) ([]Link, error) {
	q := s.Store.ReaderFor(ctx)
	out, ids, err := s.linkRows(ctx, q, id, other, self)
	if err != nil {
		return nil, err
	}
	list := make([]Task, len(ids))
	for i, tid := range ids {
		list[i] = Task{ID: tid}
	}
	if err := s.loadTags(ctx, q, list); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Tags, out[i].TagMarks = list[i].Tags, list[i].TagMarks
	}
	return out, nil
}

// linkRows reads the links of linked, without their tags, and the id of each.
func (s Store) linkRows(ctx context.Context, q tagQuerier, id, other, self string) ([]Link, []string, error) {
	rows, err := q.QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT o.id, i.slug, o.seq, o.title, o.status FROM task_deps td JOIN tasks o ON o.id = `+other+`
		JOIN initiatives i ON i.id = o.initiative_id WHERE `+self+` = ? ORDER BY o.id`), id)
	if err != nil {
		return nil, nil, fmt.Errorf("read dependencies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out, ids := []Link{}, []string{}
	for rows.Next() {
		var (
			l    Link
			tid  string
			slug string
			seq  int
		)
		if err := rows.Scan(&tid, &slug, &seq, &l.Title, &l.Status); err != nil {
			return nil, nil, fmt.Errorf("read dependencies: %w", err)
		}
		l.Ref = domain.FormatTaskRef(slug, seq)
		out, ids = append(out, l), append(ids, tid)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read dependencies: %w", err)
	}
	return out, ids, nil
}
