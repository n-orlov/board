package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // the pure-Go SQLite driver
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

// ErrNotMigrated is returned by Ready when the database does not carry every
// embedded migration.
var ErrNotMigrated = errors.New("database is not migrated")

// Store is one open database.
type Store struct {
	db         *sql.DB
	migrations fs.FS
}

// Open opens the database named by a BOARD_DB_URL value. It does not migrate.
// Only sqlite:// URLs are supported (sqlite:///data/board.db).
func Open(rawURL string) (*Store, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse BOARD_DB_URL: %w", err)
	}
	if u.Scheme != "sqlite" {
		return nil, fmt.Errorf("BOARD_DB_URL scheme %q is not supported (want sqlite://<path>)", u.Scheme)
	}
	path := u.Host + u.Path
	if path == "" {
		return nil, errors.New("BOARD_DB_URL names no database file")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	q := url.Values{}
	for _, p := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sub, err := fs.Sub(sqliteMigrations, "migrations/sqlite")
	if err != nil {
		return nil, err
	}
	return &Store{db: db, migrations: sub}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) provider() (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectSQLite3, s.db, s.migrations)
}

// Migrate applies every embedded migration that is not applied yet.
func (s *Store) Migrate(ctx context.Context) error {
	p, err := s.provider()
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Ready reports nil when the database is reachable and carries every embedded
// migration, and an error otherwise. It never writes.
func (s *Store) Ready(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	p, err := s.provider()
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	sources := p.ListSources()
	if len(sources) == 0 {
		return errors.New("no embedded migrations")
	}
	latest := sources[len(sources)-1].Version

	var applied sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied`).Scan(&applied)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return ErrNotMigrated
		}
		return fmt.Errorf("read migration version: %w", err)
	}
	if !applied.Valid || applied.Int64 < latest {
		return ErrNotMigrated
	}
	return nil
}
