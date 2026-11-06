package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/n-orlov/board/internal/audit"
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
	// WayBack and Meta are the optional Board-Actor-Way-Back and
	// Board-Actor-Meta headers of a key request.
	WayBack string
	Meta    string
	// OnBehalfOf is the optional Board-On-Behalf-Of header: the handle of the
	// actor the write is made for.
	OnBehalfOf string
}

// Event types of the key operations.
const (
	EventKeyCreated = "key_created"
	EventKeyRevoked = "key_revoked"
)

// record appends one event about the key keyID, made by who. data holds the
// type-specific fields; it never holds the key.
func record(ctx context.Context, tx *sql.Tx, d store.Dialect, who Identity, now time.Time, typ, keyID, summary string, data map[string]any) error {
	return audit.Append(ctx, tx, d, who.AuditActor(), now,
		audit.Event{Principal: who.Principal, Type: typ, RefID: keyID, Summary: summary, Data: data})
}

// AuditActor is the actor a write made by who is recorded under.
func (who Identity) AuditActor() audit.Actor {
	return audit.Actor{Handle: who.Actor, Operator: who.Operator, WayBack: who.WayBack, Meta: who.Meta, OnBehalfOf: who.OnBehalfOf}
}
