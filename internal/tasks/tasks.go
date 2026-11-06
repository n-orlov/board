package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/store"
)

// New is the request to create a task. Initiative is a slug or ULID, in either
// letter case. Kind "" is work.
type New struct {
	Initiative     string
	Title          string
	Description    string
	Kind           string
	PlannedStart   *time.Time
	PlannedEnd     *time.Time
	CadenceSeconds *int64
	NextCheckAt    *time.Time
	ExternalRefs   []domain.ExternalRef
	// BlockedBy names (by ref or ULID) the tasks the new task starts out blocked
	// by. Repeats count once.
	BlockedBy []string
}

// Normalize checks every field and returns the request with the kind defaulted,
// the title trimmed and, on a watch task, next_check_at defaulted to now plus
// the cadence.
func (n New) Normalize(now time.Time) (New, error) {
	if n.Kind == "" {
		n.Kind = domain.KindWork
	}
	if err := domain.ValidateKind(n.Kind); err != nil {
		return New{}, InvalidError{Msg: err.Error(), Next: "Send kind work or watch, or leave it out for work."}
	}
	f := fields{n.Title, n.Description, ms(n.PlannedStart), ms(n.PlannedEnd), ms(n.NextCheckAt), n.CadenceSeconds, n.ExternalRefs}
	if err := f.validate(n.Kind); err != nil {
		return New{}, err
	}
	n.Title = strings.TrimSpace(n.Title)
	n.PlannedStart, n.PlannedEnd, n.NextCheckAt = f.PlannedStart, f.PlannedEnd, f.NextChk
	if n.Kind == domain.KindWatch && n.NextCheckAt == nil {
		next := ms(&now).Add(time.Duration(*n.CadenceSeconds) * time.Second)
		n.NextCheckAt = &next
	}
	return n, nil
}

// Field is one field of a Patch. The zero value leaves the stored value, Set
// replaces it with Value and Set with Null clears it (where the field may be
// empty).
type Field[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// Patch is the request to edit a task.
type Patch struct {
	Title          Field[string]
	Description    Field[string]
	PlannedStart   Field[time.Time]
	PlannedEnd     Field[time.Time]
	CadenceSeconds Field[int64]
	NextCheckAt    Field[time.Time]
	ExternalRefs   Field[[]domain.ExternalRef]
}

func timeField(cur *time.Time, f Field[time.Time]) *time.Time {
	switch {
	case !f.Set:
		return cur
	case f.Null:
		return nil
	}
	return ms(&f.Value)
}

func intField(cur *int64, f Field[int64]) *int64 {
	switch {
	case !f.Set:
		return cur
	case f.Null:
		return nil
	}
	v := f.Value
	return &v
}

// apply returns cur with the patch laid over it. A field that cannot be empty
// refuses null.
func (p Patch) apply(cur fields) (fields, error) {
	for _, c := range []struct {
		name string
		set  bool
		null bool
	}{
		{"title", p.Title.Set, p.Title.Null},
		{"description", p.Description.Set, p.Description.Null},
		{"external_refs", p.ExternalRefs.Set, p.ExternalRefs.Null},
	} {
		if c.set && c.null {
			return fields{}, InvalidError{Msg: c.name + " cannot be null", Next: "Send " + c.name + " as a value, or leave it out to keep it."}
		}
	}
	if p.Title.Set {
		cur.Title = strings.TrimSpace(p.Title.Value)
	}
	if p.Description.Set {
		cur.Description = p.Description.Value
	}
	if p.ExternalRefs.Set {
		cur.Refs = p.ExternalRefs.Value
	}
	cur.PlannedStart = timeField(cur.PlannedStart, p.PlannedStart)
	cur.PlannedEnd = timeField(cur.PlannedEnd, p.PlannedEnd)
	cur.NextChk = timeField(cur.NextChk, p.NextCheckAt)
	cur.Cadence = intField(cur.Cadence, p.CadenceSeconds)
	return cur, nil
}

// Store reads and writes the tasks of one database.
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

var taskColumns = []string{"id", "initiative_id", "seq", "title", "description", "kind", "planned_start", "planned_end",
	"cadence_seconds", "next_check_at", "external_refs", "created_by", "created_at", "updated_at"}

const selectTask = `SELECT t.id, i.slug, t.initiative_id, t.seq, t.title, t.description, t.kind, t.status, t.resolution,
	t.planned_start, t.planned_end, t.cadence_seconds, t.next_check_at, t.external_refs,
	c.handle, t.created_at, t.updated_at, t.done_at, d.handle, t.closed_at, cl.handle,
	EXISTS (SELECT 1 FROM task_deps td JOIN tasks b ON b.id = td.blocked_by_task_id
		WHERE td.task_id = t.id AND b.status <> 'closed')
	FROM tasks t JOIN initiatives i ON i.id = t.initiative_id JOIN actors c ON c.id = t.created_by
	LEFT JOIN actors d ON d.id = t.done_by LEFT JOIN actors cl ON cl.id = t.closed_by`

// rowScanner is the part of *sql.Row and *sql.Rows that scans.
type rowScanner interface{ Scan(dest ...any) error }

func scan(r rowScanner) (Task, error) {
	var (
		t                            Task
		resolution, doneBy, closedBy sql.NullString
		start, end, cadence, next    sql.NullInt64
		done, closed                 sql.NullInt64
		created, updated             int64
		refs                         string
	)
	err := r.Scan(&t.ID, &t.Initiative, &t.InitiativeID, &t.Seq, &t.Title, &t.Description, &t.Kind, &t.Status, &resolution,
		&start, &end, &cadence, &next, &refs, &t.CreatedBy, &created, &updated, &done, &doneBy, &closed, &closedBy, &t.Blocked)
	if err != nil {
		return Task{}, err
	}
	t.Ref = domain.FormatTaskRef(t.Initiative, t.Seq)
	t.Resolution, t.DoneBy, t.ClosedBy = resolution.String, doneBy.String, closedBy.String
	t.PlannedStart, t.PlannedEnd, t.NextCheckAt = timePtr(start), timePtr(end), timePtr(next)
	t.DoneAt, t.ClosedAt = timePtr(done), timePtr(closed)
	t.CreatedAt, t.UpdatedAt = store.DecodeTime(created), store.DecodeTime(updated)
	if cadence.Valid {
		t.CadenceSeconds = &cadence.Int64
	}
	t.ExternalRefs = []domain.ExternalRef{}
	if err := json.Unmarshal([]byte(refs), &t.ExternalRefs); err != nil {
		return Task{}, fmt.Errorf("read external_refs: %w", err)
	}
	return t, nil
}

// queryer is the part of *sql.DB and *sql.Tx that reads one row.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// find reads the task a ref names through q: by id for a ULID, else by the
// initiative slug and seq. A ref that is neither form is ErrNotFound.
func (s Store) find(ctx context.Context, q queryer, ref string) (Task, error) {
	parsed, err := domain.ParseTaskRef(ref)
	if err != nil {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	where, args := ` WHERE t.id = ?`, []any{parsed.ID}
	if parsed.ID == "" {
		where, args = ` WHERE i.slug = ? AND t.seq = ?`, []any{parsed.Slug, parsed.Seq}
	}
	t, err := scan(q.QueryRowContext(ctx, s.Store.Dialect().Rebind(selectTask+where), args...))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	case err != nil:
		return Task{}, fmt.Errorf("read task: %w", err)
	}
	t.Overdue = domain.Overdue(t.Kind, t.Status, t.NextCheckAt, s.now())
	return t, nil
}

// Get returns the task whose ref (`<slug>-<seq>`) or ULID is ref, in either
// letter case, or ErrNotFound.
func (s Store) Get(ctx context.Context, ref string) (Task, error) {
	return s.find(ctx, s.Store.Reader(), ref)
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

// Create inserts the task and appends a task_created event carrying principal
// and actor, in one transaction. It returns ErrInvalid for a field that is
// wrong, ErrNoInitiative for an unknown initiative and ErrInitiativeClosed for a
// closed one, and leaves no row, event or actor behind in any of those cases.
func (s Store) Create(ctx context.Context, actor audit.Actor, principal string, in New) (Task, error) {
	now := s.now()
	in, err := in.Normalize(now)
	if err != nil {
		return Task{}, err
	}
	ini, err := initiatives.Store{Store: s.Store}.Get(ctx, in.Initiative)
	if errors.Is(err, initiatives.ErrNotFound) {
		return Task{}, fmt.Errorf("%w: %s", ErrNoInitiative, in.Initiative)
	} else if err != nil {
		return Task{}, err
	}
	id, err := domain.NewULID(now)
	if err != nil {
		return Task{}, err
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error { return s.insert(ctx, tx, actor, principal, id, ini, now, in) })
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, id)
}

// nextSeq hands out the initiative's next task number. The one UPDATE both
// counts up and refuses a closed initiative, so a seq is never reused.
func (s Store) nextSeq(ctx context.Context, tx *sql.Tx, ini initiatives.Initiative) (int, error) {
	var seq int
	err := tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(
		`UPDATE initiatives SET next_task_seq = next_task_seq + 1 WHERE id = ? AND status = 'open' RETURNING next_task_seq - 1`), ini.ID).Scan(&seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("%w: %s", ErrInitiativeClosed, ini.Slug)
	case err != nil:
		return 0, fmt.Errorf("number task: %w", err)
	}
	return seq, nil
}

// insert writes the row and the event of a new task in tx.
func (s Store) insert(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, id string, ini initiatives.Initiative, now time.Time, in New) error {
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	seq, err := s.nextSeq(ctx, tx, ini)
	if err != nil {
		return err
	}
	refs, err := encodeRefs(in.ExternalRefs)
	if err != nil {
		return err
	}
	at := store.EncodeTime(now)
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("tasks", taskColumns)),
		id, ini.ID, seq, in.Title, in.Description, in.Kind, optionalTime(in.PlannedStart), optionalTime(in.PlannedEnd),
		optionalInt(in.CadenceSeconds), optionalTime(in.NextCheckAt), refs, actorID, at, at)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	ref := domain.FormatTaskRef(ini.Slug, seq)
	err = audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventCreated, InitiativeID: ini.ID, TaskID: id, RefID: id,
		Summary: "task " + ref + " created",
		Data:    map[string]any{"ref": ref, "initiative": ini.Slug, "title": in.Title, "kind": in.Kind},
	})
	if err != nil {
		return err
	}
	return s.insertBlockers(ctx, tx, actorID, principal, Task{ID: id, Ref: ref, InitiativeID: ini.ID}, in.BlockedBy)
}

// insertBlockers makes the new task blocked by each task refs names, once each,
// and appends a dep_added event for it. A new task has no dependents, so none of
// these can close a cycle. An unknown task is ErrNotFound.
func (s Store) insertBlockers(ctx context.Context, tx *sql.Tx, actorID, principal string, task Task, refs []string) error {
	seen := map[string]bool{}
	for _, ref := range refs {
		blocker, err := s.find(ctx, tx, ref)
		if err != nil {
			return err
		}
		if seen[blocker.ID] {
			continue
		}
		seen[blocker.ID] = true
		_, err = tx.ExecContext(ctx, s.Store.Dialect().Rebind(`INSERT INTO task_deps (task_id, blocked_by_task_id) VALUES (?, ?)`), task.ID, blocker.ID)
		if err != nil {
			return fmt.Errorf("write dependency: %w", err)
		}
		if err := s.appendDep(ctx, tx, actorID, principal, task, blocker, EventDepAdded, "now blocked by"); err != nil {
			return err
		}
	}
	return nil
}

// Patch applies the edit to the task ref names and returns the task. A patch
// that changes something writes the row and a task_updated event whose
// data.changed names the changed fields, in one transaction. A patch that
// changes nothing writes nothing, not even an actor. It returns ErrNotFound for
// an unknown task, ErrClosed for a closed one, ErrInitiativeClosed when its
// initiative is closed and ErrInvalid for a value that is wrong.
func (s Store) Patch(ctx context.Context, actor audit.Actor, principal, ref string, p Patch) (Task, error) {
	var out Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.patch(ctx, tx, actor, principal, ref, p)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

func (s Store) patch(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, p Patch) (Task, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Task{}, err
	}
	if err := s.writable(ctx, tx, cur); err != nil {
		return Task{}, err
	}
	next, err := p.apply(cur.fields())
	if err != nil {
		return Task{}, err
	}
	if err := next.validate(cur.Kind); err != nil {
		return Task{}, err
	}
	names := changed(cur.fields(), next)
	if len(names) == 0 {
		return cur, nil
	}
	now := s.now()
	if err := s.update(ctx, tx, actor, principal, cur, next, names, now); err != nil {
		return Task{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// writable returns an error unless the task and its initiative are open for
// edits.
func (s Store) writable(ctx context.Context, tx *sql.Tx, t Task) error {
	if t.Status == "closed" {
		return fmt.Errorf("%w: %s", ErrClosed, t.Ref)
	}
	var status string
	err := tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT status FROM initiatives WHERE id = ?`), t.InitiativeID).Scan(&status)
	switch {
	case err != nil:
		return fmt.Errorf("read initiative: %w", err)
	case status == "closed":
		return fmt.Errorf("%w: %s", ErrInitiativeClosed, t.Initiative)
	}
	return nil
}

// update writes the new fields of cur and the task_updated event.
func (s Store) update(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, cur Task, next fields, names []string, now time.Time) error {
	d := s.Store.Dialect()
	refs, err := encodeRefs(next.Refs)
	if err != nil {
		return err
	}
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(`UPDATE tasks SET title = ?, description = ?, planned_start = ?, planned_end = ?,
		cadence_seconds = ?, next_check_at = ?, external_refs = ?, updated_at = ? WHERE id = ?`),
		next.Title, next.Description, optionalTime(next.PlannedStart), optionalTime(next.PlannedEnd),
		optionalInt(next.Cadence), optionalTime(next.NextChk), refs, store.EncodeTime(now), cur.ID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	return audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventUpdated, InitiativeID: cur.InitiativeID, TaskID: cur.ID, RefID: cur.ID,
		Summary: "task " + cur.Ref + " updated: " + strings.Join(names, ", "),
		Data:    map[string]any{"ref": cur.Ref, "changed": names},
	})
}
