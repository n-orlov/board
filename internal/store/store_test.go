package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open("sqlite://" + filepath.Join(t.TempDir(), "sub", "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestReadyOnlyAfterMigrate(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.Ready(ctx); !errors.Is(err, ErrNotMigrated) {
		t.Fatalf("Ready on a fresh database = %v, want ErrNotMigrated", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(ctx); err != nil {
		t.Fatalf("Ready after Migrate = %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate = %v, want idempotent", err)
	}
}

func TestReadyFailsWhenClosed(t *testing.T) {
	s := open(t)
	_ = s.Close()
	if err := s.Ready(context.Background()); err == nil {
		t.Fatal("Ready on a closed database = nil")
	}
}

func TestOpenRejectsOtherSchemes(t *testing.T) {
	if _, err := Open("mysql://x/y"); err == nil {
		t.Fatal("expected an unsupported scheme error")
	}
	if _, err := Open("sqlite://"); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func TestOpenSQLiteFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The database's directory would have to live under a regular file.
	_, err := Open("sqlite://" + filepath.Join(file, "sub", "board.db"))
	if err == nil || !strings.Contains(err.Error(), "create database directory") {
		t.Fatalf("Open = %v, want a create-directory error", err)
	}
}

func TestOpenSQLiteFailsWhenThePathIsADirectory(t *testing.T) {
	if _, err := Open("sqlite://" + t.TempDir()); err == nil {
		t.Fatal("Open on a directory path succeeded")
	}
}

func pragma(t *testing.T, c *sql.Conn, name string) string {
	t.Helper()
	var v string
	if err := c.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

// TestSQLitePragmasOnEveryConnection reads the four required settings back
// from the writer and from every connection the read pool opens. It fails
// when any one of them is dropped from the DSN.
func TestSQLitePragmasOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	want := map[string]string{
		"journal_mode": "wal",
		"busy_timeout": "5000",
		"foreign_keys": "1",
		"synchronous":  "1", // NORMAL
	}
	check := func(label string, c *sql.Conn) {
		for name, w := range want {
			if got := strings.ToLower(pragma(t, c, name)); got != w {
				t.Errorf("%s: PRAGMA %s = %q, want %q", label, name, got, w)
			}
		}
	}
	wc, err := s.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer wc.Close()
	check("writer", wc)

	for i := 0; i < 3; i++ { // held at once, so the pool opens distinct connections
		c, err := s.Reader().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		check(fmt.Sprintf("reader %d", i), c)
	}
}

func TestSQLiteUsesOneWriterAndAReadPool(t *testing.T) {
	s := open(t)
	if got := s.Writer().Stats().MaxOpenConnections; got != 1 {
		t.Errorf("writer MaxOpenConnections = %d, want 1", got)
	}
	if got := s.Reader().Stats().MaxOpenConnections; got < 2 {
		t.Errorf("reader MaxOpenConnections = %d, want a pool of at least 2", got)
	}
	if _, err := s.Reader().Exec("CREATE TABLE x (a INT)"); err == nil {
		t.Error("the read pool accepted a write")
	}
}

func columns(t *testing.T, s *Store, table string) map[string]bool {
	t.Helper()
	rows, err := s.Reader().Query("SELECT name FROM pragma_table_info('" + table + "')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		cols[n] = true
	}
	return cols
}

// TestMigrateCreatesThePhase0Tables checks the SPEC 4 columns of the tables in use.
func TestMigrateCreatesThePhase0Tables(t *testing.T) {
	s := open(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"actors": {"n", "id", "handle", "kind", "display_name", "host", "way_back", "meta",
			"first_seen_at", "last_seen_at", "protocol_seen"},
		"initiatives": {"n", "id", "slug", "title", "description", "status", "target_start", "target_end",
			"created_by", "created_at", "closed_at", "closed_by", "next_task_seq"},
		"initiative_tags": {"initiative_id", "tag"},
		"events": {"n", "id", "at", "actor_id", "principal", "on_behalf_of", "initiative_id", "task_id",
			"type", "ref_id", "summary", "data"},
		"api_keys": {"n", "id", "name", "key_hash", "display_prefix", "created_at", "created_by",
			"last_used_at", "expires_at", "revoked_at"},
		"sessions": {"id_hash", "email", "csrf_token", "created_at", "last_seen_at", "expires_at"},
		"tasks": {"n", "id", "initiative_id", "seq", "title", "description", "kind", "status", "resolution",
			"planned_start", "planned_end", "cadence_seconds", "next_check_at", "external_refs",
			"created_by", "created_at", "updated_at", "done_at", "done_by", "closed_at", "closed_by"},
		"task_deps": {"task_id", "blocked_by_task_id"},
		"notes": {"n", "id", "actor_id", "summary", "body", "task_id", "initiative_id", "resource", "mentions",
			"created_at"},
		"cursors": {"actor_id", "stream", "position"},
	}
	for table, cols := range want {
		got := columns(t, s, table)
		if len(got) != len(cols) {
			t.Errorf("%s has %d columns, want %d", table, len(got), len(cols))
		}
		for _, c := range cols {
			if !got[c] {
				t.Errorf("%s lacks column %s", table, c)
			}
		}
	}
}

func TestShortRefIdentityIsNeverReused(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ins := s.Dialect().InsertReturning("actors",
		[]string{"id", "handle", "kind", "first_seen_at", "last_seen_at"}, "n")
	var first, second int64
	w := s.Writer()
	if err := w.QueryRowContext(ctx, ins, "A1", "a", "agent", 1, 1).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, "DELETE FROM actors WHERE id = 'A1'"); err != nil {
		t.Fatal(err)
	}
	if err := w.QueryRowContext(ctx, ins, "A2", "b", "agent", 1, 1).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if second <= first {
		t.Fatalf("n %d was reused or went backwards after %d", second, first)
	}
}

func TestOneOperatorActorOnly(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ins := s.Dialect().Insert("actors", []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"})
	if _, err := s.Writer().ExecContext(ctx, ins, "A1", "op", "operator", 1, 1); err != nil {
		t.Fatal(err)
	}
	_, err := s.Writer().ExecContext(ctx, ins, "A2", "op2", "operator", 1, 1)
	if err == nil || !s.Dialect().IsUniqueViolation(err) {
		t.Fatalf("a second operator = %v, want a unique violation", err)
	}
}

func setVersion(t *testing.T, s *Store, v int) {
	t.Helper()
	if _, err := s.Writer().Exec("INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", v); err != nil {
		t.Fatal(err)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatalf("CheckSchema on a current database = %v", err)
	}
	setVersion(t, s, 9999)
	for name, err := range map[string]error{
		"CheckSchema": s.CheckSchema(ctx),
		"Migrate":     s.Migrate(ctx),
		"Ready":       s.Ready(ctx),
	} {
		if !errors.Is(err, ErrSchemaNewer) {
			t.Errorf("%s = %v, want ErrSchemaNewer", name, err)
			continue
		}
		if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "9999") {
			t.Errorf("%s error %q is not one line naming the version", name, err)
		}
	}
}

func TestSchemaVersionOfAnEmptyDatabaseIsZero(t *testing.T) {
	s := open(t)
	if v, err := s.SchemaVersion(context.Background()); err != nil || v != 0 {
		t.Fatalf("SchemaVersion = %d, %v", v, err)
	}
	if err := s.CheckSchema(context.Background()); err != nil {
		t.Fatalf("CheckSchema on an empty database = %v", err)
	}
}

func TestEmbeddedMigrationsCoverBothDialects(t *testing.T) {
	for _, name := range []string{"sqlite", "postgres"} {
		mig, err := dialectMigrations(name)
		if err != nil {
			t.Fatal(err)
		}
		files, err := fs.Glob(mig, "*.sql")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 9 || files[8] != "00009_notifications.sql" || files[7] != "00008_ask_end_commit.sql" || files[6] != "00007_ask_ended_at.sql" || files[5] != "00006_notes.sql" || files[4] != "00005_asks.sql" || files[3] != "00004_leases.sql" || files[0] != "00001_core_tables.sql" || files[1] != "00002_join_tables_sessions_indexes.sql" || files[2] != "00003_tasks.sql" {
			t.Errorf("%s migrations = %v", name, files)
		}
		if _, err := fs.ReadFile(mig, files[1]); err != nil {
			t.Errorf("%s: reading the common migration: %v", name, err)
		}
		if _, err := mig.Open("nope.sql"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: opening a missing file = %v", name, err)
		}
	}
}

func TestOpenPostgresIsLazy(t *testing.T) {
	s, err := Open("postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Dialect().Name() != "postgres" || s.Reader() != s.Writer() {
		t.Fatalf("postgres store: dialect %s, shared pool %v", s.Dialect().Name(), s.Reader() == s.Writer())
	}
	if err := s.Ready(context.Background()); err == nil {
		t.Fatal("Ready against a closed port = nil")
	}
}
