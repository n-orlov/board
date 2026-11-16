package cli

import (
	"context"
	"testing"

	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/store"
)

func inboxCreator(t *testing.T, st *store.Store) (handle, kind, principal string) {
	t.Helper()
	err := st.Reader().QueryRowContext(context.Background(), `SELECT a.handle, a.kind, e.principal FROM events e
		JOIN actors a ON a.id = e.actor_id WHERE e.type = 'initiative_created'`).Scan(&handle, &kind, &principal)
	if err != nil {
		t.Fatal(err)
	}
	return handle, kind, principal
}

// TestBootstrapInboxCreatesTheInboxAsTheOperatorActor starts a migrated store with
// BOARD_OPERATOR_HANDLE set and unset: the inbox exists afterwards, created by the
// operator actor under system:bootstrap, and a second call changes nothing.
func TestBootstrapInboxCreatesTheInboxAsTheOperatorActor(t *testing.T) {
	for _, tc := range []struct{ name, env, want string }{{"set", "boss", "boss"}, {"unset", "", DefaultOperatorHandle}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open(sqliteURL(t))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			env := func(name string) string {
				if name == "BOARD_OPERATOR_HANDLE" {
					return tc.env
				}
				return ""
			}
			for range 2 {
				if err := bootstrapInbox(ctx, st, env); err != nil {
					t.Fatal(err)
				}
			}
			inbox, err := (&initiatives.Store{Store: st}).Get(ctx, "inbox")
			if err != nil || inbox.Title != "Inbox" {
				t.Fatalf("inbox = %+v, err %v", inbox, err)
			}
			if handle, kind, principal := inboxCreator(t, st); handle != tc.want || kind != "operator" || principal != "system:bootstrap" {
				t.Fatalf("event by %s (%s) under %s, want %s (operator) under system:bootstrap", handle, kind, principal, tc.want)
			}
		})
	}
}

// TestBootstrapInboxWaitsOnADatabaseThatIsNotMigrated leaves an empty database alone.
func TestBootstrapInboxWaitsOnADatabaseThatIsNotMigrated(t *testing.T) {
	st, err := store.Open(sqliteURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := bootstrapInbox(context.Background(), st, func(string) string { return "" }); err != nil {
		t.Fatalf("bootstrapInbox on an unmigrated database: %v", err)
	}
}
