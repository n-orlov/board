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

// execer is the part of *sql.DB and *sql.Tx that writes.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// newRow makes a key and its row for name. The plaintext is returned once. A
// ttl above zero sets expires_at.
func (k Keys) newRow(name, createdBy string, ttl time.Duration) (string, Key, error) {
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
	if ttl > 0 {
		at := now.Add(ttl)
		row.ExpiresAt = &at
	}
	return plain, row, nil
}

// Create makes a key called name and returns it with its row. The plaintext
// is returned once and stored nowhere: the row keeps its hash and display
// prefix. A ttl above zero sets expires_at. It records no event: it is the
// command line's path, which has no actor.
func (k Keys) Create(ctx context.Context, name, createdBy string, ttl time.Duration) (string, Key, error) {
	plain, row, err := k.newRow(name, createdBy, ttl)
	if err != nil {
		return "", Key{}, err
	}
	if err := k.insert(ctx, k.Store.Writer(), row, HashKey(plain)); err != nil {
		return "", Key{}, err
	}
	return plain, row, nil
}

// CreateAs is Create for a request: created_by is who's principal, and the
// creation is appended to the events with that principal and who's actor, in
// the same transaction as the row.
func (k Keys) CreateAs(ctx context.Context, who Identity, name string, ttl time.Duration) (string, Key, error) {
	plain, row, err := k.newRow(name, who.Principal, ttl)
	if err != nil {
		return "", Key{}, err
	}
	data := map[string]any{"name": row.Name, "expires_at": nil}
	if row.ExpiresAt != nil {
		data["expires_at"] = row.ExpiresAt.Format(time.RFC3339)
	}
	err = k.inTx(ctx, func(tx *sql.Tx) error {
		if err := k.insert(ctx, tx, row, HashKey(plain)); err != nil {
			return err
		}
		return record(ctx, tx, k.Store.Dialect(), who, row.CreatedAt, EventKeyCreated, row.ID, "key "+row.Name+" created", data)
	})
	if err != nil {
		return "", Key{}, err
	}
	return plain, row, nil
}

// inTx runs f in a transaction on the writer and commits when f returns nil.
func (k Keys) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := k.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (k Keys) insert(ctx context.Context, ex execer, row Key, hash string) error {
	d := k.Store.Dialect()
	var expires any
	if row.ExpiresAt != nil {
		expires = store.EncodeTime(*row.ExpiresAt)
	}
	_, err := ex.ExecContext(ctx, d.Rebind(d.Insert("api_keys", keyColumns)),
		row.ID, row.Name, hash, row.DisplayPrefix, store.EncodeTime(row.CreatedAt), row.CreatedBy, expires)
	switch {
	case d.IsUniqueViolation(err):
		return fmt.Errorf("%w: %s", ErrKeyExists, row.Name)
	case err != nil:
		return fmt.Errorf("create key: %w", err)
	}
	return nil
}

// Revoke sets revoked_at on the key called name and reports whether it was
// already revoked, which leaves the first revocation time alone. It records no
// event: it is the command line's path.
func (k Keys) Revoke(ctx context.Context, name string) (already bool, err error) {
	err = k.inTx(ctx, func(tx *sql.Tx) error {
		_, already, err = k.revoke(ctx, tx, name)
		return err
	})
	return already, err
}

// RevokeAs is Revoke for a request: a revocation that happens is appended to
// the events with who's principal and actor, in the same transaction. It
// returns the key as it stands afterwards.
func (k Keys) RevokeAs(ctx context.Context, who Identity, name string) (key Key, already bool, err error) {
	err = k.inTx(ctx, func(tx *sql.Tx) error {
		var id string
		id, already, err = k.revoke(ctx, tx, name)
		if err != nil || already {
			return err
		}
		return record(ctx, tx, k.Store.Dialect(), who, k.now(), EventKeyRevoked, id, "key "+name+" revoked", map[string]any{"name": name})
	})
	if err != nil {
		return Key{}, false, err
	}
	key, err = k.Get(ctx, name)
	return key, already, err
}

// revoke revokes the key called name inside tx and returns its id.
func (k Keys) revoke(ctx context.Context, tx *sql.Tx, name string) (id string, already bool, err error) {
	d := k.Store.Dialect()
	var revoked sql.NullInt64
	err = tx.QueryRowContext(ctx, d.Rebind(`SELECT id, revoked_at FROM api_keys WHERE name = ?`), name).Scan(&id, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("%w: %s", ErrKeyNotFound, name)
	}
	if err != nil {
		return "", false, fmt.Errorf("revoke key: %w", err)
	}
	if revoked.Valid {
		return id, true, nil
	}
	if _, err = tx.ExecContext(ctx, d.Rebind(`UPDATE api_keys SET revoked_at = ? WHERE id = ?`), store.EncodeTime(k.now()), id); err != nil {
		return "", false, fmt.Errorf("revoke key: %w", err)
	}
	return id, false, nil
}

// Get returns the key called name.
func (k Keys) Get(ctx context.Context, name string) (Key, error) {
	keys, err := k.query(ctx, ` WHERE name = ?`, name)
	if err != nil {
		return Key{}, err
	}
	if len(keys) == 0 {
		return Key{}, fmt.Errorf("%w: %s", ErrKeyNotFound, name)
	}
	return keys[0], nil
}

// List returns every key, oldest first.
func (k Keys) List(ctx context.Context) ([]Key, error) {
	return k.query(ctx, ``)
}

func (k Keys) query(ctx context.Context, where string, args ...any) ([]Key, error) {
	d := k.Store.Dialect()
	rows, err := k.Store.Reader().QueryContext(ctx, d.Rebind(
		`SELECT id, name, display_prefix, created_at, created_by, last_used_at, expires_at, revoked_at FROM api_keys`+where+` ORDER BY n`), args...)
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
