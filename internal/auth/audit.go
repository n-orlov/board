package auth

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

// Identity is who made a request (SPEC 3.1): the principal that authenticated,
// `key:<name>` or `oidc:<email>`, and the actor it acts as. Both are recorded,
// in separate columns, on the events of a write.
type Identity struct {
	Principal string
	Actor     string
	// Operator is set for a browser session, whose actor is the operator.
	Operator bool
}

// Event types of the key operations.
const (
	EventKeyCreated = "key_created"
	EventKeyRevoked = "key_revoked"
)

var (
	actorColumns = []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"}
	eventColumns = []string{"id", "at", "actor_id", "principal", "type", "ref_id", "summary", "data"}
)

// upsertActor makes sure the actor of who exists and returns its id. The row
// is created on first use of a handle and last_seen_at moves forward on every
// use; an existing row keeps its kind.
func upsertActor(ctx context.Context, tx *sql.Tx, d store.Dialect, who Identity, now time.Time) (string, error) {
	id, err := domain.NewULID(now)
	if err != nil {
		return "", err
	}
	kind := "agent"
	if who.Operator {
		kind = "operator"
	}
	at := store.EncodeTime(now)
	_, err = tx.ExecContext(ctx, d.Rebind(d.Upsert("actors", actorColumns, []string{"handle"}, []string{"last_seen_at"})),
		id, strings.TrimSpace(who.Actor), kind, at, at)
	if err != nil {
		return "", fmt.Errorf("record actor: %w", err)
	}
	var actorID string
	err = tx.QueryRowContext(ctx, d.Rebind(`SELECT id FROM actors WHERE handle = ?`), strings.TrimSpace(who.Actor)).Scan(&actorID)
	if err != nil {
		return "", fmt.Errorf("record actor: %w", err)
	}
	return actorID, nil
}

// record appends one event about the key keyID, made by who. data holds the
// type-specific fields; it never holds the key.
func record(ctx context.Context, tx *sql.Tx, d store.Dialect, who Identity, now time.Time, typ, keyID, summary string, data map[string]any) error {
	actorID, err := upsertActor(ctx, tx, d, who, now)
	if err != nil {
		return err
	}
	id, err := domain.NewULID(now)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("events", eventColumns)),
		id, store.EncodeTime(now), actorID, who.Principal, typ, keyID, summary, string(payload))
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}
