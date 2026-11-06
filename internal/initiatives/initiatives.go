package initiatives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// EventCreated is the type of the event a creation appends.
const EventCreated = "initiative_created"

// Errors a caller tells apart.
var (
	ErrInvalid   = errors.New("invalid initiative")
	ErrExists    = errors.New("initiative already exists")
	ErrNotFound  = errors.New("initiative not found")
	ErrBadCursor = errors.New("unknown cursor")
)

// InvalidError is a field the request got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// Initiative is one initiatives row with its tags. Ref is the slug.
type Initiative struct {
	ID          string
	Slug        string
	Title       string
	Description string
	Status      string
	Tags        []string
	TargetStart string
	TargetEnd   string
	CreatedBy   string
	CreatedAt   time.Time
	ClosedAt    *time.Time
	ClosedBy    string
}

// New is the request to create an initiative.
type New struct {
	Slug        string
	Title       string
	Description string
	Tags        []string
	TargetStart string
	TargetEnd   string
}

// Normalize checks every field and returns the request with its title trimmed
// and its tags normalised.
func (n New) Normalize() (New, error) {
	tags, tagErr := domain.NormalizeTags(n.Tags)
	for _, c := range []struct {
		err  error
		next string
	}{
		{domain.ValidateSlug(n.Slug), NextSlug},
		{domain.ValidateTitle(n.Title), "Send a title of 1 to 200 characters."},
		{domain.ValidateDescription(n.Description), "Shorten the description to at most 20000 characters."},
		{tagErr, NextTags},
		{domain.ValidateTargets(n.TargetStart, n.TargetEnd), "Send target_start and target_end as YYYY-MM-DD dates, the end not before the start."},
	} {
		if c.err != nil {
			return New{}, InvalidError{Msg: c.err.Error(), Next: c.next}
		}
	}
	n.Title = strings.TrimSpace(n.Title)
	n.Tags = tags
	return n, nil
}

// What to send instead, for the two fields a caller most often gets wrong.
const (
	NextSlug = "Send a slug of 2 to 40 characters of a-z, 0-9 and -, such as phone-onboard."
	NextTags = "Send at most 20 tags, each of a-z, 0-9, . _ : and -, starting with a letter or digit."
)

// The status filters of a list. The empty status is StatusOpen.
const (
	StatusOpen   = "open"
	StatusClosed = "closed"
	StatusAll    = "all"
)

// Query selects a page of initiatives. Tags keeps only initiatives that carry
// every one of them. Limit 0 is the default page size.
type Query struct {
	Tags  []string
	After domain.Cursor
	Limit int
	// Status is open (the default when empty), closed or all. ClosedWithin is 7d,
	// 30d, 90d or an integer of days and needs a Status of closed or all.
	Status       string
	ClosedWithin string
}

// Page is one page of a list. NextAfter is the cursor of the following page,
// empty on the last one.
type Page struct {
	Items     []Initiative
	NextAfter string
}

// Store reads and writes the initiatives of one database.
type Store struct {
	Store *store.Store
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// optional is the column value of an optional text field.
func optional(v string) any {
	if v == "" {
		return nil
	}
	return v
}

var (
	initiativeColumns = []string{"id", "slug", "title", "description", "target_start", "target_end", "created_by", "created_at"}
	selectInitiative  = `SELECT i.id, i.slug, i.title, i.description, i.status, i.target_start, i.target_end,
		c.handle, i.created_at, i.closed_at, cl.handle
		FROM initiatives i JOIN actors c ON c.id = i.created_by LEFT JOIN actors cl ON cl.id = i.closed_by`
)

// Create inserts the initiative and its tags and appends an initiative_created
// event carrying principal and actor, in one transaction. It returns
// ErrInvalid for a field that is wrong and ErrExists for a taken slug, and
// leaves no row, tag, event or actor behind in either case.
func (s Store) Create(ctx context.Context, actor audit.Actor, principal string, in New) (Initiative, error) {
	in, err := in.Normalize()
	if err != nil {
		return Initiative{}, err
	}
	now := s.now()
	id, err := domain.NewULID(now)
	if err != nil {
		return Initiative{}, err
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error { return s.insert(ctx, tx, actor, principal, id, now, in) })
	if err != nil {
		return Initiative{}, err
	}
	return s.getBy(ctx, "i.id", id)
}

// insert writes the row, the tags and the event of a new initiative in tx.
func (s Store) insert(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, id string, now time.Time, in New) error {
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("initiatives", initiativeColumns)),
		id, in.Slug, in.Title, in.Description, optional(in.TargetStart), optional(in.TargetEnd), actorID, store.EncodeTime(now))
	switch {
	case d.IsUniqueViolation(err):
		return fmt.Errorf("%w: %s", ErrExists, in.Slug)
	case err != nil:
		return fmt.Errorf("create initiative: %w", err)
	}
	for _, tag := range in.Tags {
		if _, err := tx.ExecContext(ctx, d.Rebind(d.Insert("initiative_tags", []string{"initiative_id", "tag"})), id, tag); err != nil {
			return fmt.Errorf("tag initiative: %w", err)
		}
	}
	return audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventCreated, InitiativeID: id, RefID: id,
		Summary: "initiative " + in.Slug + " created",
		Data:    map[string]any{"slug": in.Slug, "title": in.Title, "tags": in.Tags},
	})
}

// inTx runs f in a transaction on the writer and commits when f returns nil.
func (s Store) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// rowScanner is the part of *sql.Row and *sql.Rows that scans.
type rowScanner interface{ Scan(dest ...any) error }

func scan(r rowScanner) (Initiative, error) {
	var (
		i                    Initiative
		start, end, closedBy sql.NullString
		created              int64
		closed               sql.NullInt64
	)
	err := r.Scan(&i.ID, &i.Slug, &i.Title, &i.Description, &i.Status, &start, &end, &i.CreatedBy, &created, &closed, &closedBy)
	if err != nil {
		return Initiative{}, err
	}
	i.TargetStart, i.TargetEnd, i.ClosedBy = start.String, end.String, closedBy.String
	i.CreatedAt = store.DecodeTime(created)
	if closed.Valid {
		at := store.DecodeTime(closed.Int64)
		i.ClosedAt = &at
	}
	i.Tags = []string{}
	return i, nil
}

// Get returns the initiative whose ref (slug) or ULID is ref, in either letter
// case, or ErrNotFound. The row is found by looking the text up as an id and as
// a slug (domain.RefLookups), not by guessing its kind from its shape.
func (s Store) Get(ctx context.Context, ref string) (Initiative, error) {
	return s.find(ctx, s.Store.Reader(), ref)
}

// find is Get reading through q, which may be a transaction.
func (s Store) find(ctx context.Context, q querier, ref string) (Initiative, error) {
	for _, l := range domain.RefLookups(ref) {
		column := "i.slug"
		if l.ByID {
			column = "i.id"
		}
		i, err := s.getVia(ctx, q, column, l.Value)
		if !errors.Is(err, ErrNotFound) {
			return i, err
		}
	}
	return Initiative{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
}

// querier is the part of *sql.DB and *sql.Tx that reads.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// getBy reads the one initiative whose column (i.id or i.slug) equals value.
func (s Store) getBy(ctx context.Context, column, value string) (Initiative, error) {
	return s.getVia(ctx, s.Store.Reader(), column, value)
}

// getVia is getBy reading through q, which may be a transaction.
func (s Store) getVia(ctx context.Context, q querier, column, value string) (Initiative, error) {
	d := s.Store.Dialect()
	i, err := scan(q.QueryRowContext(ctx, d.Rebind(selectInitiative+` WHERE `+column+` = ?`), value))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Initiative{}, fmt.Errorf("%w: %s", ErrNotFound, value)
	case err != nil:
		return Initiative{}, fmt.Errorf("read initiative: %w", err)
	}
	tags, err := s.tagsVia(ctx, q, []string{i.ID})
	if err != nil {
		return Initiative{}, err
	}
	i.Tags = tags[i.ID]
	return i, nil
}

// tags returns the sorted tags of each initiative id; an id with none maps to
// an empty slice.
func (s Store) tags(ctx context.Context, ids []string) (map[string][]string, error) {
	return s.tagsVia(ctx, s.Store.Reader(), ids)
}

// tagsVia is tags reading through q, which may be a transaction.
func (s Store) tagsVia(ctx context.Context, q querier, ids []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for _, id := range ids {
		out[id] = []string{}
	}
	d := s.Store.Dialect()
	query := `SELECT initiative_id, tag FROM initiative_tags WHERE initiative_id IN (` + marks(len(ids)) + `) ORDER BY initiative_id, tag`
	rows, err := q.QueryContext(ctx, d.Rebind(query), anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, fmt.Errorf("read tags: %w", err)
		}
		out[id] = append(out[id], tag)
	}
	return out, rows.Err()
}

func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

func anys(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

// after resolves a cursor to the id the page starts after, "" for the first
// page. A reference names the initiative whose id or slug it is; a ULID that
// names no row still pages (ids are keys, so it starts after that id), and any
// other text that names no initiative is ErrBadCursor.
func (s Store) after(ctx context.Context, c domain.Cursor) (string, error) {
	if c.IsZero() {
		return "", nil
	}
	got, err := s.Get(ctx, c.Ref)
	switch {
	case err == nil:
		return got.ID, nil
	case !errors.Is(err, ErrNotFound):
		return "", err
	}
	if id, ok := domain.KeysetULID(c.Ref); ok {
		return id, nil
	}
	return "", fmt.Errorf("%w: there is no initiative %s", ErrBadCursor, c.Ref)
}

// List returns one page of initiatives ordered by id, each with its tags. Tags
// and the size are checked here: a tag that is not a valid tag, or a limit out
// of range, is ErrInvalid.
func (s Store) List(ctx context.Context, q Query) (Page, error) {
	tags, err := domain.NormalizeTags(q.Tags)
	if err != nil {
		return Page{}, InvalidError{Msg: err.Error(), Next: NextTags}
	}
	size, err := domain.PageSize(q.Limit)
	if err != nil {
		return Page{}, InvalidError{Msg: err.Error(), Next: "Send a limit between 1 and 1000, or leave it out for 100."}
	}
	after, err := s.after(ctx, q.After)
	if err != nil {
		return Page{}, err
	}
	where, args, err := s.filter(tags, after, q)
	if err != nil {
		return Page{}, err
	}
	items, err := s.rows(ctx, selectInitiative+where+` ORDER BY i.id LIMIT `+fmt.Sprint(size+1), args)
	if err != nil {
		return Page{}, err
	}
	return s.page(ctx, items, size)
}

// rows runs a query of selectInitiative's columns and scans every row.
func (s Store) rows(ctx context.Context, query string, args []any) ([]Initiative, error) {
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list initiatives: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []Initiative{}
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list initiatives: %w", err)
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list initiatives: %w", err)
	}
	return items, nil
}

// filter is the WHERE clause that keeps initiatives after id, carrying every
// tag and matching the status filters of q, with its arguments.
func (s Store) filter(tags []string, after string, q Query) (string, []any, error) {
	var conds []string
	var args []any
	if after != "" {
		conds = append(conds, `i.id > ?`)
		args = append(args, after)
	}
	if len(tags) > 0 {
		conds = append(conds, `(SELECT COUNT(*) FROM initiative_tags t WHERE t.initiative_id = i.id AND t.tag IN (`+marks(len(tags))+`)) = ?`)
		args = append(args, anys(tags)...)
		args = append(args, len(tags))
	}
	cond, extra, err := s.statusCond(q)
	if err != nil {
		return "", nil, err
	}
	if cond != "" {
		conds = append(conds, cond)
		args = append(args, extra...)
	}
	if len(conds) == 0 {
		return "", nil, nil
	}
	return ` WHERE ` + strings.Join(conds, ` AND `), args, nil
}

// NextClosedWithin says how to send closed_within.
const NextClosedWithin = "Send closed_within as 7d, 30d, 90d or a number of days, with status closed or all."

// statusName is the status filter s, open when empty.
func statusName(s string) (string, error) {
	switch s {
	case "":
		return StatusOpen, nil
	case StatusOpen, StatusClosed, StatusAll:
		return s, nil
	}
	return "", InvalidError{Msg: fmt.Sprintf("status %q is not an initiative status filter", s),
		Next: "Send status open, closed or all, or leave it out for open."}
}

// statusCond is the condition for the status and closed_within filters, with its
// arguments; empty for status all without closed_within.
func (s Store) statusCond(q Query) (string, []any, error) {
	status, err := statusName(q.Status)
	if err != nil {
		return "", nil, err
	}
	if q.ClosedWithin != "" {
		return closedWithinCond(status, q.ClosedWithin, s.now())
	}
	if status == StatusAll {
		return "", nil, nil
	}
	return `i.status = ?`, []any{status}, nil
}

// closedWithinCond is the condition that keeps the initiatives closed in the last
// window days, and with status all also the ones not closed.
func closedWithinCond(status, window string, now time.Time) (string, []any, error) {
	days, err := domain.ParseDays(window)
	if err != nil {
		return "", nil, InvalidError{Msg: err.Error(), Next: NextClosedWithin}
	}
	since := store.EncodeTime(now.AddDate(0, 0, -days))
	switch status {
	case StatusClosed:
		return `i.closed_at >= ?`, []any{since}, nil
	case StatusAll:
		return `(i.status <> 'closed' OR i.closed_at >= ?)`, []any{since}, nil
	}
	return "", nil, InvalidError{Msg: "closed_within needs status closed or all", Next: NextClosedWithin}
}

// page cuts items (read with one row more than size) to the page and fills in
// their tags and the next cursor.
func (s Store) page(ctx context.Context, items []Initiative, size int) (Page, error) {
	p := Page{}
	if len(items) > size {
		items = items[:size]
		p.NextAfter = items[size-1].ID
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	tags, err := s.tags(ctx, ids)
	if err != nil {
		return Page{}, err
	}
	for i := range items {
		items[i].Tags = tags[items[i].ID]
	}
	p.Items = items
	return p, nil
}
