package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func eventCount(t *testing.T, k Keys, typ string) int {
	t.Helper()
	var n int
	if err := k.Store.Reader().QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE type = ?`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateAsRecordsTheEventInOneTransactionWithTheRow(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	who := Identity{Principal: "key:ralphd", Actor: "claude@host"}
	_, row, err := k.CreateAs(ctx, who, "laptop", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var principal, handle, kind, typ, ref string
	err = k.Store.Reader().QueryRowContext(ctx,
		`SELECT e.principal, a.handle, a.kind, e.type, e.ref_id FROM events e JOIN actors a ON a.id = e.actor_id`).Scan(&principal, &handle, &kind, &typ, &ref)
	if err != nil || principal != "key:ralphd" || handle != "claude@host" || kind != "agent" || typ != EventKeyCreated || ref != row.ID {
		t.Fatalf("event: %q %q %q %q %q (%v)", principal, handle, kind, typ, ref, err)
	}
	if row.CreatedBy != "key:ralphd" || row.ExpiresAt == nil || !row.ExpiresAt.Equal(t0.Add(24*time.Hour)) {
		t.Errorf("row = %+v", row)
	}
	// A taken name rolls the whole transaction back: no second event, no actor.
	if _, _, err := k.CreateAs(ctx, Identity{Principal: "key:ralphd", Actor: "other"}, "laptop", 0); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("duplicate = %v, want ErrKeyExists", err)
	}
	var actors int
	_ = k.Store.Reader().QueryRowContext(ctx, `SELECT count(*) FROM actors`).Scan(&actors)
	if eventCount(t, k, EventKeyCreated) != 1 || actors != 1 {
		t.Errorf("a refused create left %d events and %d actors", eventCount(t, k, EventKeyCreated), actors)
	}
	if _, _, err := k.CreateAs(ctx, who, "bad name", 0); !errors.Is(err, ErrInvalidName) {
		t.Errorf("bad name = %v", err)
	}
}

func TestAnActorIsUpsertedOnUseAndKeepsItsKind(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	k.Now = func() time.Time { return t0 }
	for i, name := range []string{"a", "b"} {
		k.Now = func() time.Time { return t0.Add(time.Duration(i) * time.Hour) }
		if _, _, err := k.CreateAs(ctx, Identity{Principal: "oidc:x@y", Actor: "operator", Operator: true}, name, 0); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	var first, last int64
	err := k.Store.Reader().QueryRowContext(ctx, `SELECT count(*), min(first_seen_at), max(last_seen_at) FROM actors WHERE handle = 'operator' AND kind = 'operator'`).Scan(&n, &first, &last)
	if err != nil || n != 1 || last-first != int64(time.Hour/time.Millisecond) {
		t.Errorf("actors %d first %d last %d (%v)", n, first, last, err)
	}
}

func TestRevokeAsRecordsOnlyARevocationThatHappened(t *testing.T) {
	k := newKeys(t, t0)
	ctx := context.Background()
	who := Identity{Principal: "key:ralphd", Actor: "claude@host"}
	if _, _, err := k.RevokeAs(ctx, who, "nobody"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("unknown = %v", err)
	}
	if _, _, err := k.CreateAs(ctx, who, "laptop", 0); err != nil {
		t.Fatal(err)
	}
	key, already, err := k.RevokeAs(ctx, who, "laptop")
	if err != nil || already || key.RevokedAt == nil || key.Status(t0) != "revoked" {
		t.Fatalf("revoke = %+v %v %v", key, already, err)
	}
	_, already, err = k.RevokeAs(ctx, who, "laptop")
	if err != nil || !already || eventCount(t, k, EventKeyRevoked) != 1 {
		t.Errorf("second revoke: already=%v err=%v events=%d", already, err, eventCount(t, k, EventKeyRevoked))
	}
	if _, err := k.Get(ctx, "nobody"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Get unknown = %v", err)
	}
}

func TestParseExpiry(t *testing.T) {
	for in, want := range map[string]time.Duration{"1d": 24 * time.Hour, "12h": 12 * time.Hour, "36500d": 36500 * 24 * time.Hour} {
		if got, err := ParseExpiry(in); err != nil || got != want {
			t.Errorf("%s = %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "0d", "90", "1w", "36501d", "100000d", "-1d", " 1d"} {
		if _, err := ParseExpiry(in); err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("%q = %v, want an error naming it", in, err)
		}
	}
}
