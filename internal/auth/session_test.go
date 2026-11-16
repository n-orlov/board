package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

type clock struct{ now time.Time }

func (c *clock) get() time.Time { return c.now }

func newSessions(t *testing.T, c *clock, ttl, idle time.Duration) Sessions {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return Sessions{Store: st, TTL: ttl, Idle: idle, Now: c.get}
}

func TestSessionIsStoredOnlyAsAHashAndLooksUp(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, 0, 0)
	id, sess, err := s.Create(context.Background(), "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var hash, csrf string
	if err := s.Store.Reader().QueryRowContext(context.Background(), `SELECT id_hash, csrf_token FROM sessions`).Scan(&hash, &csrf); err != nil {
		t.Fatal(err)
	}
	if hash != HashKey(id) || hash == id || csrf != sess.CSRFToken || csrf == id {
		t.Fatalf("stored id_hash %q, csrf %q for id %q", hash, csrf, id)
	}
	if want := t0.Add(DefaultSessionTTL); !sess.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v, want %v", sess.ExpiresAt, want)
	}
	got, err := s.Lookup(context.Background(), id)
	if err != nil || got.Email != "a@example.com" || got.CSRFToken != sess.CSRFToken {
		t.Fatalf("Lookup = %+v, %v", got, err)
	}
	if _, err := s.Lookup(context.Background(), "unknown"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("unknown id: %v, want ErrNoSession", err)
	}
}

func TestSessionExpiresAbsolutelyAndWhenIdle(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, time.Hour, 10*time.Minute)
	ctx := context.Background()
	id, _, _ := s.Create(ctx, "a@example.com")
	c.now = t0.Add(9 * time.Minute)
	if _, err := s.Lookup(ctx, id); err != nil {
		t.Fatalf("within idle: %v", err)
	}
	// 9 min after the last use is within idle, so use keeps it alive until the
	// absolute limit, which ends it even though it was just used.
	c.now = t0.Add(18 * time.Minute)
	if _, err := s.Lookup(ctx, id); err != nil {
		t.Fatalf("kept alive by use: %v", err)
	}
	c.now = t0.Add(29 * time.Minute)
	if _, err := s.Lookup(ctx, id); !errors.Is(err, ErrNoSession) {
		t.Fatalf("idle for 11 min: %v, want ErrNoSession", err)
	}
}

func TestSessionEndsAtItsAbsoluteExpiryEvenWhenInUse(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, 30*time.Minute, 20*time.Minute)
	ctx := context.Background()
	id, _, _ := s.Create(ctx, "a@example.com")
	for _, minutes := range []int{9, 18, 27} {
		c.now = t0.Add(time.Duration(minutes) * time.Minute)
		if _, err := s.Lookup(ctx, id); err != nil {
			t.Fatalf("minute %d: %v", minutes, err)
		}
	}
	c.now = t0.Add(30 * time.Minute)
	if _, err := s.Lookup(ctx, id); !errors.Is(err, ErrNoSession) {
		t.Fatalf("at the absolute limit: %v, want ErrNoSession", err)
	}
}

func TestDeleteEndsTheSessionAndIsIdempotent(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, 0, 0)
	ctx := context.Background()
	id, _, _ := s.Create(ctx, "a@example.com")
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Lookup(ctx, id); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestSessionsReportAnUnusableStore(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, 0, 0)
	_ = s.Store.Close()
	if _, _, err := s.Create(context.Background(), "a@example.com"); err == nil {
		t.Fatal("Create on a closed store succeeded")
	}
	if _, err := s.Lookup(context.Background(), "x"); err == nil || errors.Is(err, ErrNoSession) {
		t.Fatalf("Lookup on a closed store: %v, want a store error", err)
	}
	if err := s.Delete(context.Background(), "x"); err == nil {
		t.Fatal("Delete on a closed store succeeded")
	}
}

func TestDeleteExpiredRemovesOnlySessionsPastTheirAbsoluteExpiry(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, time.Hour, 24*time.Hour)
	ctx := context.Background()
	old, _, _ := s.Create(ctx, "a@example.com")
	c.now = c.now.Add(30 * time.Minute)
	young, _, _ := s.Create(ctx, "b@example.com")
	if n, err := s.DeleteExpired(ctx); err != nil || n != 0 {
		t.Fatalf("nothing due: %d, %v", n, err)
	}
	c.now = t0.Add(time.Hour + time.Minute)
	if n, err := s.DeleteExpired(ctx); err != nil || n != 1 {
		t.Fatalf("one due: %d, %v", n, err)
	}
	var left int
	if err := s.Store.Reader().QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id_hash = ?`, HashKey(old)).Scan(&left); err != nil || left != 0 {
		t.Fatalf("expired session left: %d, %v", left, err)
	}
	if _, err := s.Lookup(ctx, young); err != nil {
		t.Fatalf("the live session was removed: %v", err)
	}
	_ = s.Store.Close()
	if _, err := s.DeleteExpired(ctx); err == nil {
		t.Fatal("DeleteExpired on a closed store succeeded")
	}
}

func TestDeferredSessionWritesWaitForTheCommit(t *testing.T) {
	c := &clock{t0}
	s := newSessions(t, c, time.Hour, 10*time.Minute)
	ctx := context.Background()
	id, _, _ := s.Create(ctx, "a@example.com")
	seen := func() int64 {
		var at int64 = -1
		_ = s.Store.Reader().QueryRowContext(ctx, `SELECT last_seen_at FROM sessions WHERE id_hash = ?`, HashKey(id)).Scan(&at)
		return at
	}
	c.now = t0.Add(2 * time.Minute)
	dropped, _ := WithDeferredUsage(ctx)
	if _, err := s.Lookup(dropped, id); err != nil || seen() != store.EncodeTime(t0) {
		t.Fatalf("a lookup with no commit: err %v, last_seen_at %d", err, seen())
	}
	kept, usage := WithDeferredUsage(ctx)
	if _, err := s.Lookup(kept, id); err != nil || seen() != store.EncodeTime(t0) {
		t.Fatalf("a deferred lookup wrote before its commit: err %v", err)
	}
	usage.Commit(ctx)
	if seen() != store.EncodeTime(c.now) {
		t.Fatalf("Commit wrote last_seen_at %d, want %d", seen(), store.EncodeTime(c.now))
	}
	c.now = c.now.Add(11 * time.Minute) // idle past the timeout
	dead, usage := WithDeferredUsage(ctx)
	if _, err := s.Lookup(dead, id); !errors.Is(err, ErrNoSession) || seen() < 0 {
		t.Fatalf("a deferred lookup of a dead session: err %v, row left %v", err, seen() >= 0)
	}
	usage.Commit(ctx)
	if seen() >= 0 {
		t.Fatal("Commit did not delete the dead session")
	}
}
