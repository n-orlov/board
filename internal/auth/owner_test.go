package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/n-orlov/village-hall/internal/audit"
)

func seedActors(t *testing.T, k Keys, rows ...[2]string) {
	t.Helper()
	for _, r := range rows {
		if _, err := k.Store.Writer().Exec(`INSERT INTO actors (id, handle, kind, first_seen_at, last_seen_at) VALUES (?, ?, ?, 1, 1)`, "ID-"+r[0], r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
}

func originOfEvent(t *testing.T, k Keys, typ string) string {
	t.Helper()
	var got sql.NullString
	err := k.Store.Reader().QueryRow(`SELECT o.handle FROM events e LEFT JOIN actors o ON o.id = e.origin WHERE e.type = ?`, typ).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	return got.String
}

func TestCreateOwnedNeedsAnExistingOperatorAndWritesNothingOtherwise(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	seedActors(t, k, [2]string{"alice-op", "operator"}, [2]string{"claude", "agent"})
	for _, owner := range []string{"nobody", "claude"} {
		if _, _, err := k.CreateOwned(ctx, "laptop", "admin-cli", owner, 0); !errors.Is(err, ErrOwnerNotOperator) {
			t.Fatalf("owner %q = %v, want ErrOwnerNotOperator", owner, err)
		}
	}
	if keys, err := k.List(ctx); err != nil || len(keys) != 0 {
		t.Fatalf("a refused create left keys %v (%v)", keys, err)
	}
	if _, _, err := k.CreateOwned(ctx, "bad name", "admin-cli", "alice-op", 0); !errors.Is(err, ErrInvalidName) {
		t.Errorf("bad name = %v", err)
	}
	_, row, err := k.CreateOwned(ctx, "laptop", "admin-cli", "alice-op", 0)
	if err != nil || row.Owner != "alice-op" {
		t.Fatalf("CreateOwned = %+v, %v", row, err)
	}
	if _, _, err := k.CreateOwned(ctx, "laptop", "admin-cli", "alice-op", 0); !errors.Is(err, ErrKeyExists) {
		t.Errorf("duplicate = %v, want ErrKeyExists", err)
	}
	if got, err := k.Get(ctx, "laptop"); err != nil || got.Owner != "alice-op" {
		t.Errorf("Get = %+v, %v, want owner alice-op", got, err)
	}
}

func TestCreateWithoutAnOwnerLeavesItNull(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	if _, _, err := k.Create(ctx, "plain", "admin-cli", 0); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(ctx, "plain"); err != nil || got.Owner != "" {
		t.Errorf("Get = %+v, %v, want no owner", got, err)
	}
}

func TestCreateAsOwnsTheKeyByTheOperatorBehindTheRequest(t *testing.T) {
	k := newKeys(t, t0)
	bg := context.Background()
	session := Identity{Principal: "oidc:a@example.com", Actor: "alice-op", Operator: true}
	_, row, err := k.CreateAs(bg, session, "by-session", 0)
	if err != nil || row.Owner != "alice-op" {
		t.Fatalf("session create = %+v, %v, want owner alice-op", row, err)
	}
	// The bare context carries no origin and the actor row is an operator, so
	// the event's origin is that operator itself.
	if got := originOfEvent(t, k, EventKeyCreated); got != "alice-op" {
		t.Errorf("event origin = %q, want alice-op", got)
	}
	// A key request: the owner is the owner of the key it came through.
	ctx := audit.WithOrigin(bg, "alice-op")
	_, row, err = k.CreateAs(ctx, Identity{Principal: "key:by-session", Actor: "claude"}, "child", 0)
	if err != nil || row.Owner != "alice-op" {
		t.Fatalf("key create = %+v, %v, want owner alice-op", row, err)
	}
	if got, _ := k.Get(bg, "child"); got.Owner != "alice-op" {
		t.Errorf("stored owner = %q, want alice-op", got.Owner)
	}
	// A request with no known operator, or one naming a handle that is no
	// operator, makes an ownerless key.
	for i, o := range []context.Context{audit.WithOrigin(bg, ""), audit.WithOrigin(bg, "claude"), bg} {
		name := []string{"orphan-a", "orphan-b", "orphan-c"}[i]
		_, row, err = k.CreateAs(o, Identity{Principal: "key:x", Actor: "claude"}, name, 0)
		if err != nil || row.Owner != "" {
			t.Errorf("%s = %+v, %v, want no owner", name, row, err)
		}
	}
}

func TestVerifyHandsTheKeysOwnerToTheRequest(t *testing.T) {
	k := newKeys(t, t0)
	bg := context.Background()
	seedActors(t, k, [2]string{"alice-op", "operator"})
	owned, _, err := k.CreateOwned(bg, "owned", "admin-cli", "alice-op", 0)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := k.Create(bg, "ownerless", "admin-cli", 0)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verifier{Store: k.Store, Now: k.Now}
	for key, want := range map[string]string{owned: "alice-op", plain: ""} {
		ctx, usage := WithDeferredUsage(bg)
		if _, err := v.Verify(ctx, key); err != nil {
			t.Fatal(err)
		}
		if got := usage.Owner(); got != want {
			t.Errorf("Owner = %q, want %q", got, want)
		}
	}
}

func TestCreateDefaultOwnedFallsBackToNoOwnerOnlyOnABoardWithNoOperator(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	seedActors(t, k, [2]string{"claude", "agent"})
	_, row, err := k.CreateDefaultOwned(ctx, "first", "admin-cli", "operator", 0)
	if err != nil || row.Owner != "" {
		t.Fatalf("no operator on the board = %+v, %v, want an ownerless key", row, err)
	}
	seedActors(t, k, [2]string{"alice-op", "operator"})
	for _, owner := range []string{"operator", "claude"} {
		if _, _, err := k.CreateDefaultOwned(ctx, "second", "admin-cli", owner, 0); !errors.Is(err, ErrOwnerNotOperator) {
			t.Fatalf("default %q on an established board = %v, want ErrOwnerNotOperator", owner, err)
		}
	}
	if keys, err := k.List(ctx); err != nil || len(keys) != 1 {
		t.Fatalf("a refused create left keys %v (%v)", keys, err)
	}
	if _, row, err := k.CreateDefaultOwned(ctx, "third", "admin-cli", "alice-op", 0); err != nil || row.Owner != "alice-op" {
		t.Errorf("default naming an operator = %+v, %v", row, err)
	}
	if _, _, err := k.CreateDefaultOwned(ctx, "bad name", "admin-cli", "alice-op", 0); !errors.Is(err, ErrInvalidName) {
		t.Errorf("bad name = %v", err)
	}
}
