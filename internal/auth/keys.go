package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Errors a caller tells apart.
var (
	ErrKeyExists   = errors.New("key name already exists")
	ErrKeyNotFound = errors.New("key not found")
	ErrInvalidName = errors.New("key name must be 1-64 characters of a-z, A-Z, 0-9, '.', '_' or '-', starting with a letter or digit")
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateName reports whether name may name a key. A name is typed back into
// commands and appears in URL paths, so it holds no separator or space.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// Key is one api_keys row. It never holds the key itself.
type Key struct {
	ID            string
	Name          string
	DisplayPrefix string
	CreatedAt     time.Time
	CreatedBy     string
	LastUsedAt    *time.Time
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
}

// Status is "revoked", "expired" or "active" at the instant now.
func (k Key) Status(now time.Time) string {
	switch {
	case k.RevokedAt != nil:
		return "revoked"
	case k.ExpiresAt != nil && !now.Before(*k.ExpiresAt):
		return "expired"
	}
	return "active"
}

// Keys reads and writes the api_keys table of one store.
type Keys struct {
	Store *store.Store
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (k Keys) now() time.Time {
	if k.Now != nil {
		return k.Now().UTC()
	}
	return time.Now().UTC()
}

var keyColumns = []string{"id", "name", "key_hash", "display_prefix", "created_at", "created_by", "expires_at"}

// Create makes a key called name and returns it with its row. The plaintext
// is returned once and stored nowhere: the row keeps its hash and display
// prefix. A ttl above zero sets expires_at.
func (k Keys) Create(ctx context.Context, name, createdBy string, ttl time.Duration) (string, Key, error) {
	if err := ValidateName(name); err != nil {
		return "", Key{}, err
	}
	now := k.now()
	plain, err := NewKey()
	if err != nil {
		return "", Key{}, err
	}
	id, err := domain.NewULID(now)
	if err != nil {
		return "", Key{}, err
	}
	row := Key{ID: id, Name: name, DisplayPrefix: DisplayPrefix(plain), CreatedAt: now, CreatedBy: createdBy}
	var expires any
	if ttl > 0 {
		at := now.Add(ttl)
		row.ExpiresAt, expires = &at, store.EncodeTime(at)
	}
	if err := k.insert(ctx, row, HashKey(plain), expires); err != nil {
		return "", Key{}, err
	}
	return plain, row, nil
}

func (k Keys) insert(ctx context.Context, row Key, hash string, expires any) error {
	d := k.Store.Dialect()
	_, err := k.Store.Writer().ExecContext(ctx, d.Rebind(d.Insert("api_keys", keyColumns)),
		row.ID, row.Name, hash, row.DisplayPrefix, store.EncodeTime(row.CreatedAt), row.CreatedBy, expires)
	switch {
	case d.IsUniqueViolation(err):
		return fmt.Errorf("%w: %s", ErrKeyExists, row.Name)
	case err != nil:
		return fmt.Errorf("create key: %w", err)
	}
	return nil
}

// List returns every key, oldest first.
func (k Keys) List(ctx context.Context) ([]Key, error) {
	rows, err := k.Store.Reader().QueryContext(ctx,
		`SELECT id, name, display_prefix, created_at, created_by, last_used_at, expires_at, revoked_at FROM api_keys ORDER BY n`)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []Key
	for rows.Next() {
		key, err := scanKey(rows)
		if err != nil {
			return nil, fmt.Errorf("list keys: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	return keys, nil
}

func scanKey(rows *sql.Rows) (Key, error) {
	var key Key
	var created int64
	var used, expires, revoked sql.NullInt64
	err := rows.Scan(&key.ID, &key.Name, &key.DisplayPrefix, &created, &key.CreatedBy, &used, &expires, &revoked)
	key.CreatedAt = store.DecodeTime(created)
	key.LastUsedAt, key.ExpiresAt, key.RevokedAt = nullTime(used), nullTime(expires), nullTime(revoked)
	return key, err
}

func nullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := store.DecodeTime(v.Int64)
	return &t
}

// Revoke sets revoked_at on the key called name and reports whether it was
// already revoked, which leaves the first revocation time alone.
func (k Keys) Revoke(ctx context.Context, name string) (already bool, err error) {
	d := k.Store.Dialect()
	res, err := k.Store.Writer().ExecContext(ctx,
		d.Rebind(`UPDATE api_keys SET revoked_at = ? WHERE name = ? AND revoked_at IS NULL`),
		store.EncodeTime(k.now()), name)
	if err != nil {
		return false, fmt.Errorf("revoke key: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	var found int
	err = k.Store.Writer().QueryRowContext(ctx,
		d.Rebind(`SELECT count(*) FROM api_keys WHERE name = ?`), name).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("revoke key: %w", err)
	}
	if found == 0 {
		return false, fmt.Errorf("%w: %s", ErrKeyNotFound, name)
	}
	return true, nil
}
