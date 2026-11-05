package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

var (
	actorColumns = []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"}
	eventColumns = []string{"id", "at", "actor_id", "principal", "initiative_id", "type", "ref_id", "summary", "data"}
)

// Actor is who a write acts as: a handle, and whether it is the operator.
type Actor struct {
	Handle   string
	Operator bool
}

// Event is one row of the append-only events table. InitiativeID and RefID are
// empty when the event has none. Data holds the type-specific fields.
type Event struct {
	Principal    string
	Type         string
	InitiativeID string
	RefID        string
	Summary      string
	Data         map[string]any
}

// UpsertActor makes sure the actor exists and returns its id. The row is
// created on first use of a handle and last_seen_at moves forward on every
// use; an existing row keeps its kind.
func UpsertActor(ctx context.Context, tx *sql.Tx, d store.Dialect, a Actor, now time.Time) (string, error) {
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
	return actorID, nil
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
func AppendAs(ctx context.Context, tx *sql.Tx, d store.Dialect, actorID string, now time.Time, e Event) error {
	id, err := domain.NewULID(now)
	if err != nil {
		return err
	}
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("events", eventColumns)),
		id, store.EncodeTime(now), actorID, e.Principal, nullable(e.InitiativeID), e.Type, nullable(e.RefID), e.Summary, string(payload))
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}
