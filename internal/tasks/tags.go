package tasks

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/store"
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

// loadTags sets Tags and TagMarks on every task of list: Tags from task_tags, sorted, and to an
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
	inherited, err := s.initiativeTagsByTask(ctx, q, ids)
	if err != nil {
		return err
	}
	kinds, err := s.actorKindsByTask(ctx, q, ids)
	if err != nil {
		return err
	}
	for i := range list {
		id := list[i].ID
		list[i].Tags = append([]string{}, byTask[id]...)
		list[i].TagMarks = tagMarks(byTask[id], inherited[id], kinds[id])
	}
	return nil
}

// tagMarks is the task's own, initiative and actor-kind tags as marks, in that
// order of source, each source's tags sorted. A tag that comes from two sources
// is marked once for each.
func tagMarks(own, initiative, actor []string) []TagMark {
	out := make([]TagMark, 0, len(own)+len(initiative)+len(actor))
	for _, g := range []struct {
		source string
		tags   []string
	}{{domain.TagSourceTask, own}, {domain.TagSourceInitiative, initiative}, {domain.TagSourceActor, actor}} {
		for _, tag := range g.tags {
			out = append(out, TagMark{Tag: tag, Source: g.source})
		}
	}
	return out
}

// initiativeTagsByTask reads the sorted tags of the initiative of each task with
// these ids.
func (s Store) initiativeTagsByTask(ctx context.Context, q tagQuerier, ids []string) (map[string][]string, error) {
	return s.pairs(ctx, q, "initiative tags",
		`SELECT t.id, it.tag FROM tasks t JOIN initiative_tags it ON it.initiative_id = t.initiative_id WHERE t.id IN (`+marks(len(ids))+`) ORDER BY t.id, it.tag`, ids, nil)
}

// actorKindsByTask reads, for each task with these ids, the actor kinds of the
// holders of every lease it has had, live, ended or expired, in
// domain.ActorKinds order (SPEC 5.5).
func (s Store) actorKindsByTask(ctx context.Context, q tagQuerier, ids []string) (map[string][]string, error) {
	held, err := s.pairs(ctx, q, "lease holders",
		`SELECT DISTINCT l.task_id, a.handle FROM leases l JOIN actors a ON a.id = l.actor_id WHERE l.task_id IN (`+marks(len(ids))+`)`, ids, domain.ActorKind)
	if err != nil {
		return nil, err
	}
	for id, kinds := range held {
		held[id] = orderedKinds(kinds)
	}
	return held, nil
}

// orderedKinds is the distinct kinds in domain.ActorKinds order.
func orderedKinds(kinds []string) []string {
	have := map[string]bool{}
	for _, k := range kinds {
		have[k] = true
	}
	var out []string
	for _, k := range domain.ActorKinds {
		if have[k] {
			out = append(out, k)
		}
	}
	return out
}

// pairs runs a query of (task id, value) rows over the ids and groups the values
// by task, in row order. When conv is set each value goes through it and one it
// turns into "" is dropped.
func (s Store) pairs(ctx context.Context, q tagQuerier, what, query string, ids []string, conv func(string) string) (map[string][]string, error) {
	rows, err := q.QueryContext(ctx, s.Store.Dialect().Rebind(query), anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var id, v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("read %s: %w", what, err)
		}
		if conv != nil {
			v = conv(v)
		}
		if v != "" {
			out[id] = append(out[id], v)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", what, err)
	}
	return out, nil
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

// Tag changes, as data.change of the task_updated event they append.
const (
	TagAdded   = "added"
	TagRemoved = "removed"
)

// AddTag gives the task ref names the tag, which is trimmed and lowercased first
// (SPEC 4.2). A change inserts the task_tags row, moves updated_at and appends a
// task_updated event whose data is {ref, changed: ["tags"], tag, change:
// "added"}, in one transaction. A task that already has the tag is returned as it
// is and nothing is written, not even an actor. It returns ErrInvalid for a bad
// tag (checked before the task is read), ErrNotFound for an unknown task,
// ErrClosed for a closed one, ErrInitiativeClosed when its initiative is closed
// and ErrInvalid for a 21st tag, and writes nothing then.
func (s Store) AddTag(ctx context.Context, actor audit.Actor, principal, ref, tag string) (Task, error) {
	return s.changeTag(ctx, actor, principal, ref, tag, TagAdded)
}

// RemoveTag takes the tag off the task ref names, the same way AddTag puts it on,
// with data.change "removed". A task that does not have the tag is returned as it
// is and nothing is written.
func (s Store) RemoveTag(ctx context.Context, actor audit.Actor, principal, ref, tag string) (Task, error) {
	return s.changeTag(ctx, actor, principal, ref, tag, TagRemoved)
}

func (s Store) changeTag(ctx context.Context, actor audit.Actor, principal, ref, tag, change string) (Task, error) {
	norm, err := normalizeTags([]string{tag})
	if err != nil {
		return Task{}, err
	}
	var out Task
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.tagChange(ctx, tx, actor, principal, ref, norm[0], change)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (s Store) tagChange(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, tag, change string) (Task, error) {
	cur, err := s.lock(ctx, tx, ref)
	if err != nil {
		return Task{}, err
	}
	if err := s.writable(ctx, tx, cur); err != nil {
		return Task{}, err
	}
	have, err := s.tagsByTask(ctx, tx, []string{cur.ID})
	if err != nil {
		return Task{}, err
	}
	if noop, err := tagDecision(have[cur.ID], tag, change); err != nil || noop {
		return cur, err
	}
	if err := s.writeTagChange(ctx, tx, actor, principal, cur, tag, change); err != nil {
		return Task{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// tagDecision is true when the change leaves the tags as they are: adding one
// the task has, or removing one it lacks. Adding a 21st tag is an InvalidError.
func tagDecision(have []string, tag, change string) (bool, error) {
	for _, h := range have {
		if h == tag {
			return change == TagAdded, nil
		}
	}
	if change == TagRemoved {
		return true, nil
	}
	if len(have) >= domain.MaxTags {
		return false, InvalidError{Msg: fmt.Sprintf("a task carries at most %d tags", domain.MaxTags), Next: initiatives.NextTags}
	}
	return false, nil
}

// writeTagChange writes the row, moves updated_at and appends the event, with the
// actor upserted.
func (s Store) writeTagChange(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, cur Task, tag, change string) error {
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	stmt, verb := `INSERT INTO task_tags (task_id, tag) VALUES (?, ?)`, "added tag"
	if change == TagRemoved {
		stmt, verb = `DELETE FROM task_tags WHERE task_id = ? AND tag = ?`, "removed tag"
	}
	if _, err := tx.ExecContext(ctx, d.Rebind(stmt), cur.ID, tag); err != nil {
		return fmt.Errorf("write task tag: %w", err)
	}
	if _, err := tx.ExecContext(ctx, d.Rebind(`UPDATE tasks SET updated_at = ? WHERE id = ?`), store.EncodeTime(now), cur.ID); err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventUpdated, InitiativeID: cur.InitiativeID, TaskID: cur.ID, RefID: cur.ID,
		Summary: "task " + cur.Ref + " " + verb + " " + tag,
		Data:    map[string]any{"ref": cur.Ref, "changed": []string{"tags"}, "tag": tag, "change": change},
	})
}
