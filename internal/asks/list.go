package asks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
)

// State filters of a list. The empty state is StateOpen.
const (
	StateOpen      = domain.AskOpen
	StateAnswered  = domain.AskAnswered
	StateWithdrawn = domain.AskWithdrawn
	StateAll       = "all"
)

// Query selects a page of asks. State is open (the default), answered,
// withdrawn or all; Actor is the asker's handle, Task a task ref or ULID and
// Initiative a slug or ULID, each empty for no filter. After is the ref (A<n>)
// or ULID of the last ask of the previous page; Limit 0 is the default page
// size.
type Query struct {
	State      string
	Actor      string
	Task       string
	Initiative string
	After      string
	Limit      int
}

// Page is one page of asks ordered by id. NextAfter is the ULID to send as After
// for the following page, empty on the last page.
type Page struct {
	Items     []Ask
	NextAfter string
}

// List returns the page of asks q selects, ordered by id. It returns ErrInvalid
// for a state that is none of the four, a limit out of range or an after that
// names no ask, and ErrNotFound for a task or initiative filter naming none.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	size, err := domain.PageSize(q.Limit)
	if err != nil {
		return Page{}, InvalidError{Msg: err.Error(), Next: "Send a limit between 1 and 1000, or leave it out for 100."}
	}
	f, err := s.conditions(ctx, q)
	if err != nil {
		return Page{}, err
	}
	items, err := s.rows(ctx, f, size+1)
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

// state adds the condition for a state filter: open when none is sent.
func (f *filter) state(state string) error {
	switch state {
	case "", StateOpen:
		f.add(`k.state = 'open'`)
	case StateAnswered, StateWithdrawn:
		f.add(`k.state = ?`, state)
	case StateAll:
	default:
		return InvalidError{Msg: fmt.Sprintf("state %q is not an ask state", state),
			Next: "Send state=open, answered, withdrawn or all, or leave state out for open."}
	}
	return nil
}

func (s Store) conditions(ctx context.Context, q Query) (*filter, error) {
	f := &filter{}
	if err := f.state(q.State); err != nil {
		return nil, err
	}
	if q.Actor != "" {
		f.add(`a.handle = ?`, q.Actor)
	}
	if err := s.scopeFilter(ctx, f, q); err != nil {
		return nil, err
	}
	if err := s.afterFilter(ctx, f, q.After); err != nil {
		return nil, err
	}
	return f, nil
}

// scopeFilter adds the task and initiative conditions, which need the database
// to resolve a ref to an id.
func (s Store) scopeFilter(ctx context.Context, f *filter, q Query) error {
	if q.Task != "" {
		t, err := s.findTask(ctx, s.Store.ReaderFor(ctx), q.Task)
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

// afterFilter adds the condition for the ask an after cursor names: its ref
// (A<n>) or its ULID. A ULID that names no ask still pages (it is a position).
func (s Store) afterFilter(ctx context.Context, f *filter, after string) error {
	if after == "" {
		return nil
	}
	if _, ok := domain.ParseAskRef(after); ok {
		k, err := s.Get(ctx, after)
		if err != nil {
			return InvalidError{Msg: fmt.Sprintf("after %q names no ask", after), Next: "Send the next_after of the previous page, or an ask ref such as A3."}
		}
		f.add(`k.id > ?`, k.ID)
		return nil
	}
	id, ok := domain.KeysetULID(after)
	if !ok {
		return InvalidError{Msg: fmt.Sprintf("after %q is not an ask ref or id", after), Next: "Send the next_after of the previous page, or an ask ref such as A3."}
	}
	f.add(`k.id > ?`, id)
	return nil
}

func (s Store) rows(ctx context.Context, f *filter, limit int) ([]Ask, error) {
	query := selectAsk
	if len(f.conds) > 0 {
		query += ` WHERE ` + strings.Join(f.conds, ` AND `)
	}
	rows, err := s.Store.ReaderFor(ctx).QueryContext(ctx, s.Store.Dialect().Rebind(query+` ORDER BY k.id LIMIT `+fmt.Sprint(limit)), f.args...)
	if err != nil {
		return nil, fmt.Errorf("list asks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Ask{}
	for rows.Next() {
		k, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list asks: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list asks: %w", err)
	}
	return out, nil
}
