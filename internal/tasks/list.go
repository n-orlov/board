package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/store"
)

// The status filters of a list. The empty status is StatusNotClosed.
const (
	StatusOpen      = "open"
	StatusDone      = "done"
	StatusClosed    = "closed"
	StatusNotClosed = "not_closed"
	StatusAll       = "all"
)

// ErrBadCursor is an after value that names no task.
var ErrBadCursor = errors.New("unknown cursor")

// Query selects a page of tasks ordered by id.
//
// Initiatives are slugs or ULIDs: a task is kept when it belongs to any of them.
// Tags are initiative tags: the task's initiative must carry every one. Status
// is open, done, closed, not_closed (the default when empty) or all, and Kind
// work or watch, empty for either. Active, Blocked and Overdue keep the tasks
// whose derived state equals the value, nil for either. ClosedWithin is 7d, 30d,
// 90d or an integer of days and needs a status of closed or all. Q is a text the
// title or the description contains, in either letter case. After is the ULID or
// ref of the last task of the previous page. Limit 0 is the default page size.
type Query struct {
	Initiatives  []string
	Tags         []string
	Status       string
	Kind         string
	Active       *bool
	Blocked      *bool
	Overdue      *bool
	ClosedWithin string
	Q            string
	After        string
	Limit        int
}

// Page is one page of tasks. NextAfter is the ULID to send as After for the
// following page, empty on the last page.
type Page struct {
	Items     []Task
	NextAfter string
}

// List returns the page of tasks q selects. It returns ErrInvalid for a field
// that is wrong, ErrNoInitiative for an initiative that names nothing and
// ErrBadCursor for an after that names no task. It reads the tasks as they are:
// a lease that has run out is expired by the caller first (leases.Store.ExpireDue).
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	size, err := domain.PageSize(q.Limit)
	if err != nil {
		return Page{}, InvalidError{Msg: err.Error(), Next: "Send a limit between 1 and 1000, or leave it out for 100."}
	}
	f := &filter{now: s.now()}
	if err := s.conditions(ctx, f, q); err != nil {
		return Page{}, err
	}
	query := selectTask + f.where() + ` ORDER BY t.id LIMIT ` + fmt.Sprint(size+1)
	items, err := s.rows(ctx, query, f.args, f.now)
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

// rows runs a query of selectTask's columns and scans every row.
func (s Store) rows(ctx context.Context, query string, args []any, now time.Time) ([]Task, error) {
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []Task{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}
		t.Overdue = domain.Overdue(t.Kind, t.Status, t.NextCheckAt, now)
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return items, nil
}

// filter collects the WHERE conditions of a list and their arguments. now is
// the instant the derived states are judged at.
type filter struct {
	conds []string
	args  []any
	now   time.Time
}

func (f *filter) add(cond string, args ...any) {
	f.conds = append(f.conds, cond)
	f.args = append(f.args, args...)
}

func (f *filter) where() string {
	if len(f.conds) == 0 {
		return ""
	}
	return ` WHERE ` + strings.Join(f.conds, ` AND `)
}

// flag adds cond when want is true and its negation when it is false.
func (f *filter) flag(want *bool, cond string, args ...any) {
	switch {
	case want == nil:
	case *want:
		f.add(cond, args...)
	default:
		f.add(`NOT `+cond, args...)
	}
}

func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

func anys(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

// conditions turns the query into WHERE conditions and their arguments.
func (s Store) conditions(ctx context.Context, f *filter, q Query) error {
	if err := s.scope(ctx, f, q); err != nil {
		return err
	}
	if err := f.statusAndKind(q); err != nil {
		return err
	}
	f.flag(q.Active, `EXISTS (SELECT 1 FROM leases l WHERE l.task_id = t.id AND l.state = 'live' AND l.expires_at >= ?)`, store.EncodeBound(f.now))
	f.flag(q.Blocked, `EXISTS (SELECT 1 FROM task_deps td JOIN tasks b ON b.id = td.blocked_by_task_id
		WHERE td.task_id = t.id AND b.status <> 'closed')`)
	f.flag(q.Overdue, `(t.kind = 'watch' AND t.status <> 'closed' AND t.next_check_at IS NOT NULL AND t.next_check_at < ?)`, store.EncodeBound(f.now))
	f.text(q.Q)
	return s.afterFilter(ctx, f, q.After)
}

// scope adds the initiative and tag conditions.
func (s Store) scope(ctx context.Context, f *filter, q Query) error {
	if len(q.Initiatives) > 0 {
		ids := make([]string, 0, len(q.Initiatives))
		for _, ref := range q.Initiatives {
			ini, err := initiatives.Store{Store: s.Store, Now: s.Now}.Get(ctx, ref)
			if errors.Is(err, initiatives.ErrNotFound) {
				return fmt.Errorf("%w: %s", ErrNoInitiative, ref)
			} else if err != nil {
				return err
			}
			ids = append(ids, ini.ID)
		}
		f.add(`t.initiative_id IN (`+marks(len(ids))+`)`, anys(ids)...)
	}
	tags, err := domain.NormalizeTags(q.Tags)
	if err != nil {
		return InvalidError{Msg: err.Error(), Next: initiatives.NextTags}
	}
	if len(tags) > 0 {
		f.add(`(SELECT COUNT(*) FROM initiative_tags it WHERE it.initiative_id = t.initiative_id AND it.tag IN (`+marks(len(tags))+`)) = ?`,
			append(anys(tags), len(tags))...)
	}
	return nil
}

// statusAndKind adds the status, closed_within and kind conditions.
func (f *filter) statusAndKind(q Query) error {
	switch q.Status {
	case "", StatusNotClosed:
		f.add(`t.status <> 'closed'`)
	case StatusOpen, StatusDone, StatusClosed:
		f.add(`t.status = ?`, q.Status)
	case StatusAll:
	default:
		return InvalidError{Msg: fmt.Sprintf("status %q is not a task status filter", q.Status),
			Next: "Send status open, done, closed, not_closed or all, or leave it out for not_closed."}
	}
	if err := f.closedWithin(q); err != nil {
		return err
	}
	if q.Kind == "" {
		return nil
	}
	if err := domain.ValidateKind(q.Kind); err != nil {
		return InvalidError{Msg: err.Error(), Next: "Send kind work or watch, or leave it out for both."}
	}
	f.add(`t.kind = ?`, q.Kind)
	return nil
}

const nextClosedWithin = "Send closed_within as 7d, 30d, 90d or a number of days, with status closed or all."

// closedWithin adds the condition for the closed_within filter, if there is one.
func (f *filter) closedWithin(q Query) error {
	if q.ClosedWithin == "" {
		return nil
	}
	days, err := domain.ParseDays(q.ClosedWithin)
	if err != nil {
		return InvalidError{Msg: err.Error(), Next: nextClosedWithin}
	}
	since := store.EncodeTime(f.now.AddDate(0, 0, -days))
	switch q.Status {
	case StatusClosed:
		f.add(`t.closed_at >= ?`, since)
	case StatusAll:
		f.add(`(t.status <> 'closed' OR t.closed_at >= ?)`, since)
	default:
		return InvalidError{Msg: "closed_within needs status closed or all", Next: nextClosedWithin}
	}
	return nil
}

// text adds the condition that the title or the description contains q, ignoring
// letter case. The pattern's own % and _ match themselves.
func (f *filter) text(q string) {
	if q == "" {
		return
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q))
	pattern := "%" + esc + "%"
	f.add(`(LOWER(t.title) LIKE ? ESCAPE '\' OR LOWER(t.description) LIKE ? ESCAPE '\')`, pattern, pattern)
}

// afterFilter adds the condition that keeps the tasks after the one ref names: a
// task ref or ULID in either letter case. A ULID that names no task still pages
// (ids are keys, so it starts after that id); any other text that names no task
// is ErrBadCursor.
func (s Store) afterFilter(ctx context.Context, f *filter, ref string) error {
	if ref == "" {
		return nil
	}
	got, err := s.Get(ctx, ref)
	switch {
	case err == nil:
		f.add(`t.id > ?`, got.ID)
		return nil
	case !errors.Is(err, ErrNotFound):
		return err
	}
	if id, ok := domain.KeysetULID(ref); ok {
		f.add(`t.id > ?`, id)
		return nil
	}
	return fmt.Errorf("%w: there is no task %s", ErrBadCursor, ref)
}
