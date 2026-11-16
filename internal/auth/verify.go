package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

// Errors Verify returns for a key it checked and refused. Any other error
// means the store could not check the key.
var (
	ErrKeyUnknown = errors.New("key unknown")
	ErrKeyRevoked = errors.New("key revoked")
	ErrKeyExpired = errors.New("key expired")
)

// LastUsedInterval is the most often a key's last_used_at is written.
const LastUsedInterval = 30 * time.Second

// Verifier checks presented API keys against the api_keys table of one store.
type Verifier struct {
	Store *store.Store
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	mu   sync.Mutex
	used map[string]time.Time
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

// Verify returns the name of the key presented as plain. A key that is not
// shaped like a key, or that no row holds, is ErrKeyUnknown without a query
// for the first; a revoked or expired row is ErrKeyRevoked or ErrKeyExpired;
// a store that cannot answer gives its own error. A successful check bumps
// last_used_at at most once per LastUsedInterval per key, unless ctx came from
// WithDeferredUsage, in which case the bump waits for Usage.Commit.
func (v *Verifier) Verify(ctx context.Context, plain string) (string, error) {
	if !WellFormed(plain) {
		return "", ErrKeyUnknown
	}
	d := v.Store.Dialect()
	var key Key
	var expires, revoked sql.NullInt64
	err := v.Store.Reader().QueryRowContext(ctx,
		d.Rebind(`SELECT id, name, expires_at, revoked_at FROM api_keys WHERE key_hash = ?`), HashKey(plain)).
		Scan(&key.ID, &key.Name, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrKeyUnknown
	}
	if err != nil {
		return "", fmt.Errorf("check key: %w", err)
	}
	key.ExpiresAt, key.RevokedAt = nullTime(expires), nullTime(revoked)
	now := v.now()
	switch key.Status(now) {
	case "revoked":
		return "", ErrKeyRevoked
	case "expired":
		return "", ErrKeyExpired
	}
	later(ctx, func(ctx context.Context) { v.touch(ctx, key, now) })
	return key.Name, nil
}

type usageContextKey struct{}

// Usage holds the writes authentication makes for one request (a key's
// last_used_at, a session's last_seen_at, the removal of a dead session), so
// that they commit only if the request succeeds: a refused request changes no
// row.
type Usage struct {
	mu      sync.Mutex
	pending []func(context.Context)
}

// WithDeferredUsage returns ctx in which authentication records its writes
// instead of making them, and the Usage that makes the writes on Commit.
func WithDeferredUsage(ctx context.Context) (context.Context, *Usage) {
	u := &Usage{}
	return context.WithValue(ctx, usageContextKey{}, u), u
}

func usageFrom(ctx context.Context) *Usage {
	u, _ := ctx.Value(usageContextKey{}).(*Usage)
	return u
}

// later makes write now, or, when ctx came from WithDeferredUsage, records it
// for Commit.
func later(ctx context.Context, write func(context.Context)) {
	u := usageFrom(ctx)
	if u == nil {
		write(ctx)
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pending = append(u.pending, write)
}

// Commit makes the recorded writes, in order, and forgets them. A failed
// write is not a failed request.
func (u *Usage) Commit(ctx context.Context) {
	u.mu.Lock()
	pending := u.pending
	u.pending = nil
	u.mu.Unlock()
	for _, write := range pending {
		write(ctx)
	}
}

// touch writes last_used_at unless this key's was written within the interval.
// A failed write is not a failed request: the next use tries again.
func (v *Verifier) touch(ctx context.Context, key Key, now time.Time) {
	v.mu.Lock()
	if v.used == nil {
		v.used = map[string]time.Time{}
	}
	if last, ok := v.used[key.ID]; ok && now.Sub(last) < LastUsedInterval {
		v.mu.Unlock()
		return
	}
	v.used[key.ID] = now
	v.mu.Unlock()
	d := v.Store.Dialect()
	_, err := v.Store.Writer().ExecContext(ctx,
		d.Rebind(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`), store.EncodeTime(now), key.ID)
	if err != nil {
		v.mu.Lock()
		delete(v.used, key.ID)
		v.mu.Unlock()
	}
}
