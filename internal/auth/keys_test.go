package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/store"
)

func newKeys(t *testing.T, now time.Time) Keys {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return Keys{Store: st, Now: func() time.Time { return now }}
}

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// rawRow reads the stored columns of the key called name, as text.
func rawRow(t *testing.T, k Keys, name string) (id, hash, prefix, by string) {
	t.Helper()
	err := k.Store.Reader().QueryRowContext(context.Background(),
		`SELECT id, key_hash, display_prefix, created_by FROM api_keys WHERE name = ?`, name).Scan(&id, &hash, &prefix, &by)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestCreateStoresOnlyTheHashAndTheDisplayPrefix(t *testing.T) {
	k := newKeys(t, t0)
	plain, row, err := k.Create(context.Background(), "workstation", "admin-cli", 0)
	if err != nil {
		t.Fatal(err)
	}
	id, hash, prefix, by := rawRow(t, k, "workstation")
	if hash != HashKey(plain) || hash == plain {
		t.Fatalf("stored hash %q, want the SHA-256 of the key and never the key", hash)
	}
	if prefix != plain[len(KeyPrefix):len(KeyPrefix)+DisplayPrefixLength] || len(prefix) != 8 {
		t.Fatalf("stored prefix %q for key %q", prefix, plain)
	}
	if by != "admin-cli" || id != row.ID || len(id) != 26 {
		t.Fatalf("id %q created_by %q", id, by)
	}
	for _, col := range []string{id, hash, prefix, by} {
		if col == plain || strings.Contains(col, plain[len(KeyPrefix)+DisplayPrefixLength:]) {
			t.Fatalf("a stored column %q holds the key", col)
		}
	}
	if row.ExpiresAt != nil {
		t.Fatalf("a key made without a ttl expires at %v", row.ExpiresAt)
	}
}

func TestCreateRefusesADuplicateNameAndAnInvalidOne(t *testing.T) {
	k := newKeys(t, t0)
	if _, _, err := k.Create(context.Background(), "ralphd", "x", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.Create(context.Background(), "ralphd", "x", 0); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("duplicate: %v", err)
	}
	for _, name := range []string{"", "-lead", "has space", "a/b", strings.Repeat("a", 65)} {
		if _, _, err := k.Create(context.Background(), name, "x", 0); !errors.Is(err, ErrInvalidName) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	if _, _, err := k.Create(context.Background(), strings.Repeat("a", 64), "x", 0); err != nil {
		t.Fatalf("a 64-character name: %v", err)
	}
}

func TestListShowsKeysOldestFirstWithoutTheKeyAndTheirStatus(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	plain, _, _ := k.Create(ctx, "a", "x", 0)
	_, _, _ = k.Create(ctx, "b", "x", 24*time.Hour)
	_, _, _ = k.Create(ctx, "c", "x", 0)
	if _, err := k.Revoke(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	keys, err := k.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, key := range keys {
		names = append(names, key.Name)
		if key.ID == plain || key.DisplayPrefix == plain || key.CreatedBy != "x" || !key.CreatedAt.Equal(t0) {
			t.Fatalf("row %+v", key)
		}
	}
	if strings.Join(names, ",") != "a,b,c" {
		t.Fatalf("order %v", names)
	}
	if got := keys[1].ExpiresAt; got == nil || !got.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("b expires at %v", got)
	}
	status := func(i int, now time.Time) string { return keys[i].Status(now) }
	if status(0, t0) != "active" || status(2, t0) != "revoked" {
		t.Fatalf("statuses %s %s", status(0, t0), status(2, t0))
	}
	if status(1, t0.Add(24*time.Hour-time.Nanosecond)) != "active" || status(1, t0.Add(24*time.Hour)) != "expired" {
		t.Fatal("b should expire exactly at its expiry instant")
	}
}

func TestRevokeKeepsTheFirstRevocationAndReportsAnUnknownName(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	_, _, _ = k.Create(ctx, "laptop", "x", 0)
	if already, err := k.Revoke(ctx, "laptop"); err != nil || already {
		t.Fatalf("first revoke: already=%v err=%v", already, err)
	}
	later := k
	later.Now = func() time.Time { return t0.Add(time.Hour) }
	if already, err := later.Revoke(ctx, "laptop"); err != nil || !already {
		t.Fatalf("second revoke: already=%v err=%v", already, err)
	}
	keys, _ := k.List(ctx)
	if got := keys[0].RevokedAt; got == nil || !got.Equal(t0) {
		t.Fatalf("revoked_at %v, want the first revocation %v", got, t0)
	}
	if _, err := k.Revoke(ctx, "nope"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("unknown name: %v", err)
	}
}

func TestKeysClockDefaultsToTheWallClock(t *testing.T) {
	k := newKeys(t, t0)
	k.Now = nil
	if d := time.Since(k.now()); d < 0 || d > time.Minute {
		t.Fatalf("default clock is %v away from now", d)
	}
}
