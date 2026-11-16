package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/store"
)

// Query selects a page of notes (SPEC 6.2). Every filter combines with AND and
// an empty one selects everything. Since is a note ULID or N<n> (the notes
// after it) or an RFC 3339 time (the notes at or after that instant). Task is a
// task ref or ULID and Initiative a slug or ULID. Resources keeps the notes
// bound to any of the resources. Broadcast is "only" or "exclude". Mention and
// Author are actor handles. Full lists the bodies, else they are left out.
// After is the ref (N<n>) or ULID of the last note of the previous page; Limit 0
// is the default page size.
type Query struct {
	Since      string
	After      string
	Task       string
	Initiative string
	Resources  []string
	Broadcast  string
	Mention    string
	Author     string
	Full       bool
	Limit      int
}

// Page is one page of notes ordered by id. NextAfter is the ULID to send as
// After for the following page, empty on the last page.
type Page struct {
	Items     []Note
	NextAfter string
}

// List returns the page of notes q selects, ordered by id. It returns
// ErrInvalid for a since or after that is not readable, a broadcast that is
// neither only nor exclude or a limit out of range, and ErrNotFound for a task
// or initiative filter naming none.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	size, err := domain.PageSize(q.Limit)
	if err != nil {
		return Page{}, InvalidError{Msg: err.Error(), Next: "Send a limit between 1 and 1000, or leave it out for 100."}
	}
	f := &filter{}
	if err := s.conditions(ctx, f, q); err != nil {
		return Page{}, err
	}
	items, err := s.rows(ctx, f, size+1, q.Full)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: items}
	if len(items) > size {
		p.Items = items[:size]
		p.NextAfter = items[size-1].ID
	}
	return p, nil
}

// filter is the WHERE conditions of a query and their arguments.
type filter struct {
	conds []string
	args  []any
}

func (f *filter) add(cond string, arg ...any) {
	f.conds = append(f.conds, cond)
	f.args = append(f.args, arg...)
}

const broadcastSQL = `k.task_id IS NULL AND k.initiative_id IS NULL`

func (s Store) conditions(ctx context.Context, f *filter, q Query) error {
	if err := since(f, q.Since); err != nil {
		return err
	}
	if err := after(f, q.After); err != nil {
		return err
	}
	if err := s.scope(ctx, f, q); err != nil {
		return err
	}
	f.resources(q.Resources)
	if err := f.broadcast(q.Broadcast); err != nil {
		return err
	}
	if err := f.mention(q.Mention); err != nil {
		return err
	}
	if q.Author != "" {
		f.add(`a.handle = ?`, q.Author)
	}
	return nil
}

// broadcast adds the condition for a broadcast filter: none when it is empty.
func (f *filter) broadcast(mode string) error {
	switch mode {
	case "":
	case domain.BroadcastOnly:
		f.add(broadcastSQL)
	case domain.BroadcastExclude:
		f.add(`NOT (` + broadcastSQL + `)`)
	default:
		return InvalidError{Msg: fmt.Sprintf("broadcast %q is not only or exclude", mode),
			Next: "Send broadcast=only or broadcast=exclude, or leave it out."}
	}
	return nil
}

// mention adds the condition for a mention filter: none when it is empty.
func (f *filter) mention(handle string) error {
	if handle == "" {
		return nil
	}
	// A plain substring test on the stored JSON list: portable and
	// case-sensitive on both databases, with no LIKE wildcards to escape.
	needle, err := json.Marshal(handle)
	if err != nil {
		return fmt.Errorf("encode mention: %w", err)
	}
	f.add(`LENGTH(REPLACE(k.mentions, ?, '')) < LENGTH(k.mentions)`, string(needle))
	return nil
}

func (f *filter) resources(rs []string) {
	if len(rs) == 0 {
		return
	}
	marks := make([]string, len(rs))
	args := make([]any, len(rs))
	for i, r := range rs {
		marks[i], args[i] = "?", r
	}
	f.add(`k.resource IN (`+strings.Join(marks, ", ")+`)`, args...)
}

// scope adds the task and initiative conditions, which need the database to
// resolve a ref to an id.
func (s Store) scope(ctx context.Context, f *filter, q Query) error {
	if q.Task != "" {
		t, err := s.findTask(ctx, s.Store.Reader(), q.Task)
		if err != nil {
			return err
		}
		f.add(`k.task_id = ?`, t.ID)
	}
	if q.Initiative != "" {
		ini, err := (initiatives.Store{Store: s.Store}).Get(ctx, q.Initiative)
		if errors.Is(err, initiatives.ErrNotFound) {
			return fmt.Errorf("%w: initiative %s", ErrNotFound, q.Initiative)
		} else if err != nil {
			return err
		}
		f.add(`k.initiative_id = ?`, ini.ID)
	}
	return nil
}

const pointNext = "Send a note id, an N<n> ref such as N40, or an RFC 3339 time such as 2026-11-06T12:00:00Z."

// since adds the condition for a since value, if there is one.
func since(f *filter, value string) error {
	if value == "" {
		return nil
	}
	p, err := domain.ParseNotePoint(value)
	if err != nil {
		return InvalidError{Msg: fmt.Sprintf("since %q is not readable: %s", value, err), Next: pointNext}
	}
	switch p.Kind {
	case domain.PointN:
		f.add(`k.n > ?`, p.N)
	case domain.PointID:
		f.add(`k.id > ?`, p.ID)
	default:
		f.add(`k.created_at >= ?`, store.EncodeBound(p.At))
	}
	return nil
}

// after adds the condition for the page cursor, a note ULID or N<n>.
func after(f *filter, value string) error {
	if value == "" {
		return nil
	}
	p, err := domain.ParseNotePoint(value)
	if err != nil || p.Kind == domain.PointTime {
		return InvalidError{Msg: fmt.Sprintf("after %q is not a note ref or id", value),
			Next: "Send the next_after of the previous page, or a note ref such as N40."}
	}
	if p.Kind == domain.PointN {
		f.add(`k.n > ?`, p.N)
	} else {
		f.add(`k.id > ?`, p.ID)
	}
	return nil
}

func (s Store) rows(ctx context.Context, f *filter, limit int, full bool) ([]Note, error) {
	query := selectNote
	if len(f.conds) > 0 {
		query += ` WHERE ` + strings.Join(f.conds, ` AND `)
	}
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(query+` ORDER BY k.id LIMIT `+fmt.Sprint(limit)), f.args...)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Note{}
	for rows.Next() {
		k, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list notes: %w", err)
		}
		if !full {
			k.Body = ""
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	return out, nil
}
