package leases

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

// Start is the request to start a lease. Task is a task ref or ULID. A nil TTL
// is the default and nil Resources holds none (or, on a start that renews, keeps
// the lease's resources).
type Start struct {
	Task      string
	Intent    string
	TTL       *int64
	Resources []string
}

// Renew is the request to renew a lease. A nil field keeps the stored value.
type Renew struct {
	TTL       *int64
	Intent    *string
	Resources *[]string
}

// Result is a lease with the live leases of others that share a resource with
// it. Renewed is true when a start renewed the actor's live lease on the task.
type Result struct {
	Lease     Lease
	Conflicts []domain.Holding
	Renewed   bool
}

// taskRow is the part of a task a lease needs.
type taskRow struct {
	ID, InitiativeID, Ref, Status string
}

func (s Store) findTask(ctx context.Context, tx queryer, ref string) (taskRow, error) {
	parsed, err := domain.ParseTaskRef(ref)
	if err != nil {
		return taskRow{}, fmt.Errorf("%w: task %s", ErrNotFound, ref)
	}
	where, args := ` WHERE t.id = ?`, []any{parsed.ID}
	if parsed.ID == "" {
		where, args = ` WHERE i.slug = ? AND t.seq = ?`, []any{parsed.Slug, parsed.Seq}
	}
	var t taskRow
	var slug string
	var seq int
	err = tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT t.id, t.initiative_id, i.slug, t.seq, t.status FROM tasks t
		JOIN initiatives i ON i.id = t.initiative_id`+where), args...).Scan(&t.ID, &t.InitiativeID, &slug, &seq, &t.Status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return taskRow{}, fmt.Errorf("%w: task %s", ErrNotFound, ref)
	case err != nil:
		return taskRow{}, fmt.Errorf("read task: %w", err)
	}
	t.Ref = domain.FormatTaskRef(slug, seq)
	return t, nil
}

func invalid(err error, next string) error {
	if err == nil {
		return nil
	}
	return InvalidError{Msg: err.Error(), Next: next}
}

const (
	nextIntent    = "Send an intent of 1 to 200 characters."
	nextResources = "Send at most 20 resources of 1 to 100 characters each."
)

// Start starts a lease for the actor on the task and returns it with its
// conflicts. When the actor already holds a live lease on the task, that lease
// is renewed instead (same lease, event lease_renewed, Renewed true); otherwise
// it is a new lease numbered L<n> and the event is lease_started. It returns
// ErrInvalid for an intent or resource that is wrong (checked before anything is
// read; a missing task is ErrInvalid too), ErrNotFound for an unknown task, and ErrConflict for a done or closed task, and writes nothing then.
func (s Store) Start(ctx context.Context, actor audit.Actor, principal string, in Start) (Result, error) {
	if strings.TrimSpace(in.Task) == "" {
		return Result{}, InvalidError{Msg: "a lease needs a task", Next: "Send the ref of the task the lease is on, such as aa-1."}
	}
	if err := domain.ValidateIntent(in.Intent); err != nil {
		return Result{}, invalid(err, nextIntent)
	}
	resources, err := domain.NormalizeResources(in.Resources)
	if err != nil {
		return Result{}, invalid(err, nextResources)
	}
	in.Intent = strings.TrimSpace(in.Intent)
	var out Result
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if _, err = s.expireDue(ctx, tx, s.now()); err != nil {
			return err
		}
		out, err = s.start(ctx, tx, actor, principal, in, resources)
		return err
	})
	return out, err
}

func (s Store) start(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, in Start, resources []string) (Result, error) {
	task, err := s.findTask(ctx, tx, in.Task)
	if err != nil {
		return Result{}, err
	}
	if task.Status == "closed" {
		return Result{}, ConflictError{Msg: "cannot start a lease on task " + task.Ref + ": it is closed",
			Next: "Reopen the task first, or start the lease on an open task."}
	}
	if task.Status == "done" {
		return Result{}, ConflictError{Msg: "cannot start a lease on task " + task.Ref + ": it is done",
			Next: "Run board task reopen " + task.Ref + " first, or start the lease on an open task."}
	}
	now := s.now()
	actorID, err := audit.UpsertActor(ctx, tx, s.Store.Dialect(), actor, now)
	if err != nil {
		return Result{}, err
	}
	held, err := s.liveOf(ctx, tx, task.ID, actorID, now)
	if err != nil {
		return Result{}, err
	}
	id, err := s.startOrRenew(ctx, tx, actorID, principal, held, task, in, resources, now)
	if err != nil {
		return Result{}, err
	}
	return s.result(ctx, tx, id, now, held != "")
}

// startOrRenew renews the lease held (when it names one) with the request, or
// inserts a new lease, and returns the lease's id.
func (s Store) startOrRenew(ctx context.Context, tx *sql.Tx, actorID, principal, held string, task taskRow, in Start, resources []string, now time.Time) (string, error) {
	if held == "" {
		return s.insert(ctx, tx, actorID, principal, task, in, resources, now)
	}
	r := Renew{TTL: ttlOf(in.TTL), Intent: &in.Intent}
	if in.Resources != nil {
		r.Resources = &resources
	}
	return held, s.renew(ctx, tx, actorID, principal, held, r, task, now)
}

// ttlOf is the ttl a start asks for: the default when none was sent.
func ttlOf(requested *int64) *int64 {
	v := domain.ClampLeaseTTL(requested)
	return &v
}

// liveOf returns the id of the live, unexpired lease the actor holds on the
// task, or "".
func (s Store) liveOf(ctx context.Context, tx *sql.Tx, taskID, actorID string, now time.Time) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT id FROM leases WHERE task_id = ? AND actor_id = ?
		AND state = 'live' AND expires_at >= ?`), taskID, actorID, store.EncodeBound(now)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read lease: %w", err)
	}
	return id, nil
}

// result reads the lease id with its conflicts.
func (s Store) result(ctx context.Context, tx *sql.Tx, id string, now time.Time, renewed bool) (Result, error) {
	l, err := s.find(ctx, tx, id)
	if err != nil {
		return Result{}, err
	}
	c, err := s.conflictsOf(ctx, tx, l, now)
	return Result{Lease: l, Conflicts: c, Renewed: renewed}, err
}

func (s Store) insert(ctx context.Context, tx *sql.Tx, actorID, principal string, task taskRow, in Start, resources []string, now time.Time) (string, error) {
	d := s.Store.Dialect()
	id, err := domain.NewULID(now)
	if err != nil {
		return "", err
	}
	ttl := domain.ClampLeaseTTL(in.TTL)
	at := store.EncodeTime(now)
	expires := store.EncodeTime(now.Add(time.Duration(ttl) * time.Second))
	var n int64
	err = tx.QueryRowContext(ctx, d.Rebind(d.InsertReturning("leases",
		[]string{"id", "task_id", "actor_id", "intent", "ttl_seconds", "started_at", "renewed_at", "expires_at", "state"}, "n")),
		id, task.ID, actorID, in.Intent, ttl, at, at, expires, domain.LeaseLive).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("start lease: %w", err)
	}
	if err := s.setResources(ctx, tx, id, resources); err != nil {
		return "", err
	}
	ref := domain.FormatLeaseRef(n)
	return id, audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventStarted, InitiativeID: task.InitiativeID, TaskID: task.ID, RefID: id,
		Summary: ref + " started on " + task.Ref + ": " + domain.OneLine(in.Intent),
		Data: map[string]any{"ref": ref, "task": task.Ref, "intent": in.Intent, "ttl_seconds": ttl,
			"expires_at": store.DecodeTime(expires).Format(time.RFC3339Nano), "resources": resources},
	})
}

// setResources replaces the resources the lease holds.
func (s Store) setResources(ctx context.Context, tx *sql.Tx, id string, resources []string) error {
	d := s.Store.Dialect()
	if _, err := tx.ExecContext(ctx, d.Rebind(`DELETE FROM lease_resources WHERE lease_id = ?`), id); err != nil {
		return fmt.Errorf("set lease resources: %w", err)
	}
	for _, r := range resources {
		if _, err := tx.ExecContext(ctx, d.Rebind(d.Insert("lease_resources", []string{"lease_id", "resource"})), id, r); err != nil {
			return fmt.Errorf("set lease resources: %w", err)
		}
	}
	return nil
}

// Renew pushes the expiry of the lease ref names to now plus its ttl (the
// stored one, or the request's clamped), optionally replacing the intent and the
// resources, and appends a lease_renewed event. It returns the lease with its
// conflicts. It returns ErrInvalid for a field that is wrong, ErrNotFound for an
// unknown lease, a GoneError (ErrGone, carrying the expiry time) for an expired
// one and ErrConflict for an ended one, and writes nothing then.
func (s Store) Renew(ctx context.Context, actor audit.Actor, principal, ref string, in Renew) (Result, error) {
	in, err := normalizeRenew(in)
	if err != nil {
		return Result{}, err
	}
	var out Result
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if _, err = s.expireDue(ctx, tx, s.now()); err != nil {
			return err
		}
		out, err = s.renewRef(ctx, tx, actor, principal, ref, in)
		return err
	})
	return out, err
}

// normalizeRenew checks the intent and resources of a renewal and clamps its ttl.
func normalizeRenew(in Renew) (Renew, error) {
	if in.Intent != nil {
		if err := domain.ValidateIntent(*in.Intent); err != nil {
			return Renew{}, invalid(err, nextIntent)
		}
		v := strings.TrimSpace(*in.Intent)
		in.Intent = &v
	}
	if in.Resources != nil {
		res, err := domain.NormalizeResources(*in.Resources)
		if err != nil {
			return Renew{}, invalid(err, nextResources)
		}
		in.Resources = &res
	}
	if in.TTL != nil {
		in.TTL = ttlOf(in.TTL)
	}
	return in, nil
}

func (s Store) renewRef(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, in Renew) (Result, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Result{}, err
	}
	now := s.now()
	if err := renewable(cur, now); err != nil {
		return Result{}, err
	}
	actorID, err := audit.UpsertActor(ctx, tx, s.Store.Dialect(), actor, now)
	if err != nil {
		return Result{}, err
	}
	task, err := s.findTask(ctx, tx, cur.TaskID)
	if err != nil {
		return Result{}, err
	}
	if err := s.renew(ctx, tx, actorID, principal, cur.ID, in, task, now); err != nil {
		return Result{}, err
	}
	return s.result(ctx, tx, cur.ID, now, false)
}

// renewable returns nil for a live lease, a GoneError for an expired one and a
// ConflictError for an ended one.
func renewable(l Lease, now time.Time) error {
	switch {
	case l.Expired(now):
		return GoneError{Ref: l.Ref, ExpiredAt: l.ExpiresAt}
	case l.State == domain.LeaseEnded:
		return ConflictError{Msg: "cannot renew lease " + l.Ref + ": it is ended", Next: "Start a new lease on the task."}
	}
	return nil
}

// renew writes the new expiry (and the optional intent and resources) of the
// lease id and its lease_renewed event.
func (s Store) renew(ctx context.Context, tx *sql.Tx, actorID, principal, id string, in Renew, task taskRow, now time.Time) error {
	cur, err := s.find(ctx, tx, id)
	if err != nil {
		return err
	}
	ttl := cur.TTLSeconds
	if in.TTL != nil {
		ttl = *in.TTL
	}
	intent := cur.Intent
	if in.Intent != nil {
		intent = *in.Intent
	}
	expires := now.Add(time.Duration(ttl) * time.Second)
	d := s.Store.Dialect()
	_, err = tx.ExecContext(ctx, d.Rebind(`UPDATE leases SET ttl_seconds = ?, intent = ?, renewed_at = ?, expires_at = ? WHERE id = ?`),
		ttl, intent, store.EncodeTime(now), store.EncodeTime(expires), id)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	resources := cur.Resources
	if in.Resources != nil {
		resources = *in.Resources
		if err := s.setResources(ctx, tx, id, resources); err != nil {
			return err
		}
	}
	return audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventRenewed, InitiativeID: task.InitiativeID, TaskID: task.ID, RefID: id,
		Summary: cur.Ref + " renewed on " + task.Ref,
		Data: map[string]any{"ref": cur.Ref, "task": task.Ref, "intent": intent, "ttl_seconds": ttl,
			"expires_at": store.DecodeTime(store.EncodeTime(expires)).Format(time.RFC3339Nano), "resources": resources},
	})
}
