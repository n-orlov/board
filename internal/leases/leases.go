package leases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Event types a lease write appends.
const (
	EventStarted = "lease_started"
	EventRenewed = "lease_renewed"
	EventEnded   = "lease_ended"
	EventExpired = "lease_expired"
	EventAcked   = "lease_acked"
)

// Errors a caller tells apart.
var (
	ErrInvalid  = errors.New("invalid lease")
	ErrNotFound = errors.New("lease not found")
	ErrConflict = errors.New("lease state conflict")
	ErrGone     = errors.New("lease expired")
)

// InvalidError is a field the request got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// ConflictError is an action the lease's or task's current state does not
// allow. It satisfies errors.Is(err, ErrConflict).
type ConflictError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e ConflictError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrConflict) true for every ConflictError.
func (e ConflictError) Is(target error) bool { return target == ErrConflict }

// GoneError is a renewal of a lease that has expired. It satisfies
// errors.Is(err, ErrGone) and carries the time the lease expired at.
type GoneError struct {
	Ref       string
	ExpiredAt time.Time
}

// Error names the lease and the expiry time.
func (e GoneError) Error() string {
	return fmt.Sprintf("lease %s expired at %s", e.Ref, e.ExpiredAt.Format(time.RFC3339))
}

// Is makes errors.Is(err, ErrGone) true for every GoneError.
func (e GoneError) Is(target error) bool { return target == ErrGone }

// Lease is one leases row with its resources. Ref is L<n>; Task is the task's
// ref, and the people are actor handles. EndedBy is set only when someone other
// than the holder ended the lease.
type Lease struct {
	N          int64
	ID         string
	Ref        string
	TaskID     string
	Task       string
	Actor      string
	ActorID    string
	Intent     string
	TTLSeconds int64
	StartedAt  time.Time
	RenewedAt  time.Time
	ExpiresAt  time.Time
	State      string
	EndedAt    *time.Time
	EndedBy    string
	Outcome    string
	AckedAt    *time.Time
	AckedBy    string
	Resources  []string
}

// Holding is the lease as the conflict computation sees it.
func (l Lease) Holding() domain.Holding {
	return domain.Holding{Lease: l.Ref, Holder: l.Actor, Task: l.Task, Intent: l.Intent, ExpiresAt: l.ExpiresAt, Resources: l.Resources}
}

// Expired reports whether the lease is expired at now: marked so, or live past
// its expires_at (the sweeper has not got to it yet).
func (l Lease) Expired(now time.Time) bool {
	return l.State == domain.LeaseExpired || (l.State == domain.LeaseLive && l.ExpiresAt.Before(now))
}

// Store reads and writes the leases of one database.
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

const selectLease = `SELECT l.n, l.id, l.task_id, i.slug, t.seq, a.handle, l.actor_id, l.intent, l.ttl_seconds,
	l.started_at, l.renewed_at, l.expires_at, l.state, l.ended_at, eb.handle, l.outcome, l.acked_at, ab.handle
	FROM leases l JOIN tasks t ON t.id = l.task_id JOIN initiatives i ON i.id = t.initiative_id
	JOIN actors a ON a.id = l.actor_id LEFT JOIN actors eb ON eb.id = l.ended_by LEFT JOIN actors ab ON ab.id = l.acked_by`

// rowScanner is the part of *sql.Row and *sql.Rows that scans.
type rowScanner interface{ Scan(dest ...any) error }

// queryer is the part of *sql.DB and *sql.Tx that reads.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func scan(r rowScanner) (Lease, error) {
	var (
		l                       Lease
		slug                    string
		seq                     int
		started, renewed, exp   int64
		ended, acked            sql.NullInt64
		endedBy, outcome, ackBy sql.NullString
	)
	err := r.Scan(&l.N, &l.ID, &l.TaskID, &slug, &seq, &l.Actor, &l.ActorID, &l.Intent, &l.TTLSeconds,
		&started, &renewed, &exp, &l.State, &ended, &endedBy, &outcome, &acked, &ackBy)
	if err != nil {
		return Lease{}, err
	}
	l.Ref, l.Task = domain.FormatLeaseRef(l.N), domain.FormatTaskRef(slug, seq)
	l.StartedAt, l.RenewedAt, l.ExpiresAt = store.DecodeTime(started), store.DecodeTime(renewed), store.DecodeTime(exp)
	l.EndedAt, l.AckedAt = timePtr(ended), timePtr(acked)
	l.EndedBy, l.Outcome, l.AckedBy = endedBy.String, outcome.String, ackBy.String
	return l, nil
}

func timePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := store.DecodeTime(n.Int64)
	return &t
}

// resourcesOf lists the resources the lease id holds, sorted.
func (s Store) resourcesOf(ctx context.Context, q queryer, id string) ([]string, error) {
	rows, err := q.QueryContext(ctx, s.Store.Dialect().Rebind(`SELECT resource FROM lease_resources WHERE lease_id = ? ORDER BY resource`), id)
	if err != nil {
		return nil, fmt.Errorf("read lease resources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, fmt.Errorf("read lease resources: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// find reads the lease a ref (L<n>, either case, or its ULID) names through q.
func (s Store) find(ctx context.Context, q queryer, ref string) (Lease, error) {
	where, arg := ` WHERE l.n = ?`, any(nil)
	if n, ok := domain.ParseLeaseRef(ref); ok {
		arg = n
	} else if up := strings.ToUpper(ref); domain.ValidateULID(up) == nil {
		where, arg = ` WHERE l.id = ?`, up
	} else {
		return Lease{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	l, err := scan(q.QueryRowContext(ctx, s.Store.Dialect().Rebind(selectLease+where), arg))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Lease{}, fmt.Errorf("%w: %s", ErrNotFound, ref)
	case err != nil:
		return Lease{}, fmt.Errorf("read lease: %w", err)
	}
	l.Resources, err = s.resourcesOf(ctx, q, l.ID)
	return l, err
}

// Get returns the lease whose ref (L<n>) or ULID is ref, or ErrNotFound. It
// reads the row as stored: ExpireDue and Expired say whether it has run out.
func (s Store) Get(ctx context.Context, ref string) (Lease, error) {
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

// conflictsOf lists the live leases of others that share a resource with l, each
// with only the shared resources. An expired lease never conflicts.
func (s Store) conflictsOf(ctx context.Context, q queryer, l Lease, now time.Time) ([]domain.Holding, error) {
	ids, err := s.sharing(ctx, q, l, now)
	if err != nil {
		return nil, err
	}
	live := make([]domain.Holding, 0, len(ids))
	for _, id := range ids {
		other, err := s.find(ctx, q, id)
		if err != nil {
			return nil, err
		}
		live = append(live, other.Holding())
	}
	return domain.Conflicts(l.Ref, l.Resources, live), nil
}

// sharing lists the ids of the live, unexpired leases other than l that hold
// one of l's resources.
func (s Store) sharing(ctx context.Context, q queryer, l Lease, now time.Time) ([]string, error) {
	if len(l.Resources) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(l.Resources)), ", ")
	args := []any{store.EncodeTime(now), l.ID}
	for _, r := range l.Resources {
		args = append(args, r)
	}
	rows, err := q.QueryContext(ctx, s.Store.Dialect().Rebind(`SELECT DISTINCT r.lease_id FROM lease_resources r JOIN leases l ON l.id = r.lease_id
		WHERE l.state = 'live' AND l.expires_at >= ? AND l.id <> ? AND r.resource IN (`+marks+`) ORDER BY r.lease_id`), args...)
	if err != nil {
		return nil, fmt.Errorf("read conflicts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read conflicts: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
