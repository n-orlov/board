package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

var (
	actorColumns = []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"}
	eventColumns = []string{"id", "at", "actor_id", "principal", "on_behalf_of", "initiative_id", "task_id", "type", "ref_id", "summary", "data", "origin"}
)

// Actor is who a write acts as: a handle, and whether it is the operator.
type Actor struct {
	Handle   string
	Operator bool
	// WayBack and Meta are the optional Board-Actor-Way-Back and Board-Actor-Meta
	// the request sent. A non-empty value replaces what the row holds; an empty
	// one leaves it. Meta is a JSON object.
	WayBack string
	Meta    string
	// OnBehalfOf is the handle of the actor the Board-On-Behalf-Of header names,
	// or empty. Only the operations that record it read it.
	OnBehalfOf string
}

// Event is one row of the append-only events table. InitiativeID, TaskID and
// RefID are empty when the event has none. Data holds the type-specific fields.
type Event struct {
	Principal string
	// OnBehalfOfID is the actors row the event is made on behalf of, or empty;
	// OnBehalf returns it.
	OnBehalfOfID string
	Type         string
	InitiativeID string
	TaskID       string
	RefID        string
	Summary      string
	Data         map[string]any
}

// UpsertActor makes sure the actor exists and returns its id. The row is
// created on first use of a handle and last_seen_at moves forward on every
// use. A write that acts as the operator (a browser session) makes its row the
// operator row, promoting an agent row an earlier key write created under the
// same handle; an operator row is never demoted, so a later key write with the
// operator's handle leaves it.
//
// An agent row also takes the origin the request carries (WithOrigin): the
// owner of the key behind its latest write.
func UpsertActor(ctx context.Context, tx *sql.Tx, d store.Dialect, a Actor, now time.Time) (string, error) {
	origin, _ := OriginFrom(ctx)
	return upsertActor(ctx, tx, d, a, now, origin)
}

// upsertActor is UpsertActor with the origin handle to record on an agent row,
// or "" to leave the row's origin as it is.
func upsertActor(ctx context.Context, tx *sql.Tx, d store.Dialect, a Actor, now time.Time, origin string) (string, error) {
	id, err := domain.NewULID(now)
	if err != nil {
		return "", err
	}
	kind := "agent"
	if a.Operator {
		kind = "operator"
	}
	handle := strings.TrimSpace(a.Handle)
	at := store.EncodeTime(now)
	_, err = tx.ExecContext(ctx, d.Rebind(d.Upsert("actors", actorColumns, []string{"handle"}, []string{"last_seen_at"})),
		id, handle, kind, at, at)
	if err != nil {
		return "", fmt.Errorf("record actor: %w", err)
	}
	var actorID string
	if err := tx.QueryRowContext(ctx, d.Rebind(`SELECT id FROM actors WHERE handle = ?`), handle).Scan(&actorID); err != nil {
		return "", fmt.Errorf("record actor: %w", err)
	}
	return actorID, refineActor(ctx, tx, d, actorID, a, origin)
}

// refineActor makes the writes a use of the actor row actorID adds to its
// creation: the promotion of an agent row a session acts as, the origin and the
// way back and meta the request sent.
func refineActor(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string, a Actor, origin string) error {
	if a.Operator {
		if _, err := tx.ExecContext(ctx, d.Rebind(`UPDATE actors SET kind = 'operator' WHERE id = ? AND kind <> 'operator'`), actorID); err != nil {
			return fmt.Errorf("record actor kind: %w", err)
		}
	}
	if err := setOrigin(ctx, tx, d, actorID, origin); err != nil {
		return err
	}
	return setOptional(ctx, tx, d, actorID, a)
}

// setOptional stores the way back and meta a request sent, when it sent any.
func setOptional(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string, a Actor) error {
	for _, c := range []struct{ column, value string }{{"way_back", strings.TrimSpace(a.WayBack)}, {"meta", strings.TrimSpace(a.Meta)}} {
		if c.value == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, d.Rebind(`UPDATE actors SET `+c.column+` = ? WHERE id = ?`), c.value, actorID); err != nil {
			return fmt.Errorf("record actor %s: %w", c.column, err)
		}
	}
	return nil
}

// OnBehalf makes sure the actor a.OnBehalfOf names exists and returns its id, or
// "" when the write names none. The actor is an ordinary agent row unless an
// earlier use made it the operator's.
func OnBehalf(ctx context.Context, tx *sql.Tx, d store.Dialect, a Actor, now time.Time) (string, error) {
	handle := strings.TrimSpace(a.OnBehalfOf)
	if handle == "" {
		return "", nil
	}
	return upsertActor(ctx, tx, d, Actor{Handle: handle}, now, "")
}

// nullable is the column value of an optional text field.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Append upserts the actor and appends e, made by that actor, in tx.
func Append(ctx context.Context, tx *sql.Tx, d store.Dialect, a Actor, now time.Time, e Event) error {
	actorID, err := UpsertActor(ctx, tx, d, a, now)
	if err != nil {
		return err
	}
	return AppendAs(ctx, tx, d, actorID, now, e)
}

// AppendAs appends e, made by the actor row actorID that UpsertActor returned.
// It first takes the dialect's event-write lock, which tx holds until it ends,
// so the change feed can tell an uncommitted event number from a rolled-back one.
func AppendAs(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string, now time.Time, e Event) error {
	_, err := AppendAsID(ctx, tx, d, actorID, now, e)
	return err
}

// lockEvents takes the dialect's event-write lock, if it has one.
func lockEvents(ctx context.Context, tx *sql.Tx, d store.Dialect) error {
	lock := d.LockEventWrite()
	if lock == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, lock)
	return err
}

// AppendAsID is AppendAs that returns the id of the event it appended, for a
// write that records something about the event in the same transaction.
func AppendAsID(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string, now time.Time, e Event) (string, error) {
	id, err := domain.NewULID(now)
	if err != nil {
		return "", err
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("record event: %w", err)
	}
	origin, err := eventOrigin(ctx, tx, d, actorID)
	if err != nil {
		return "", fmt.Errorf("record event: %w", err)
	}
	if err := lockEvents(ctx, tx, d); err != nil {
		return "", fmt.Errorf("record event: %w", err)
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("events", eventColumns)),
		id, store.EncodeTime(now), actorID, e.Principal, nullable(e.OnBehalfOfID), nullable(e.InitiativeID), nullable(e.TaskID), e.Type, nullable(e.RefID), e.Summary, string(payload), origin)
	if err != nil {
		return "", fmt.Errorf("record event: %w", err)
	}
	return id, nil
}
