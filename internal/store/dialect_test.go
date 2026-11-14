package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

var (
	cols     = []string{"id", "handle", "kind"}
	allDials = []Dialect{SQLite{}, Postgres{}}
)

func TestNamesAndGooseDialects(t *testing.T) {
	if (SQLite{}).Name() != "sqlite" || (SQLite{}).GooseDialect() != goose.DialectSQLite3 {
		t.Error("sqlite identity")
	}
	if (Postgres{}).Name() != "postgres" || (Postgres{}).GooseDialect() != goose.DialectPostgres {
		t.Error("postgres identity")
	}
}

func TestPlaceholderAndRebind(t *testing.T) {
	q := "SELECT * FROM a WHERE b = ? AND c = '?' AND d = ?"
	if got := (SQLite{}).Rebind(q); got != q {
		t.Errorf("sqlite Rebind = %q", got)
	}
	if got, want := (Postgres{}).Rebind(q), "SELECT * FROM a WHERE b = $1 AND c = '?' AND d = $2"; got != want {
		t.Errorf("postgres Rebind = %q, want %q", got, want)
	}
	if (SQLite{}).Placeholder(3) != "?" || (Postgres{}).Placeholder(3) != "$3" {
		t.Error("Placeholder")
	}
}

func TestInsertAndReturning(t *testing.T) {
	for _, d := range allDials {
		if got, want := d.Insert("actors", cols), "INSERT INTO actors (id, handle, kind) VALUES (?, ?, ?)"; got != want {
			t.Errorf("%s Insert = %q", d.Name(), got)
		}
		if got, want := d.InsertReturning("actors", cols, "n", "id"),
			"INSERT INTO actors (id, handle, kind) VALUES (?, ?, ?) RETURNING n, id"; got != want {
			t.Errorf("%s InsertReturning = %q", d.Name(), got)
		}
		if got, want := d.InsertReturning("actors", cols), d.Insert("actors", cols); got != want {
			t.Errorf("%s InsertReturning with no columns = %q", d.Name(), got)
		}
	}
}

func TestUpsert(t *testing.T) {
	for _, d := range allDials {
		got := d.Upsert("actors", cols, []string{"handle"}, []string{"kind", "id"})
		want := "INSERT INTO actors (id, handle, kind) VALUES (?, ?, ?) ON CONFLICT (handle) DO UPDATE SET kind = excluded.kind, id = excluded.id"
		if got != want {
			t.Errorf("%s Upsert = %q", d.Name(), got)
		}
		got = d.Upsert("actors", cols, []string{"handle"}, nil)
		want = "INSERT INTO actors (id, handle, kind) VALUES (?, ?, ?) ON CONFLICT (handle) DO NOTHING"
		if got != want {
			t.Errorf("%s Upsert with nothing to update = %q", d.Name(), got)
		}
	}
}

func TestPartialIndex(t *testing.T) {
	for _, d := range allDials {
		if got, want := d.PartialIndex("i", "t", []string{"a", "b"}, "a IS NULL", false),
			"CREATE INDEX i ON t (a, b) WHERE a IS NULL"; got != want {
			t.Errorf("%s PartialIndex = %q", d.Name(), got)
		}
		if got, want := d.PartialIndex("i", "t", []string{"a"}, "a = 1", true),
			"CREATE UNIQUE INDEX i ON t (a) WHERE a = 1"; got != want {
			t.Errorf("%s unique PartialIndex = %q", d.Name(), got)
		}
	}
}

func TestEncodings(t *testing.T) {
	if (SQLite{}).EncodeBool(true) != 1 || (SQLite{}).EncodeBool(false) != 0 {
		t.Error("sqlite stores booleans as 0 and 1")
	}
	if (Postgres{}).EncodeBool(true) != true || (Postgres{}).EncodeBool(false) != false {
		t.Error("postgres stores booleans as booleans")
	}
	at := time.Date(2026, 11, 5, 12, 30, 15, 123_456_789, time.FixedZone("x", 3600))
	ms := EncodeTime(at)
	if ms != 1_793_878_215_123 {
		t.Errorf("EncodeTime = %d, want UTC milliseconds since the epoch", ms)
	}
	back := DecodeTime(ms)
	if back.Location() != time.UTC || back.UnixMilli() != at.UnixMilli() || back.Nanosecond() != 123_000_000 {
		t.Errorf("DecodeTime(%d) = %v, want UTC at millisecond precision", ms, back)
	}
}

func TestListenNotifyOnlyOnPostgres(t *testing.T) {
	if (SQLite{}).SupportsListenNotify() || !(Postgres{}).SupportsListenNotify() {
		t.Error("LISTEN/NOTIFY availability")
	}
}

func TestErrorClassification(t *testing.T) {
	pgMissing := &pgconn.PgError{Code: "42P01"}
	pgDup := &pgconn.PgError{Code: "23505"}
	if !(Postgres{}).IsMissingTable(pgMissing) || (Postgres{}).IsMissingTable(pgDup) || (Postgres{}).IsMissingTable(nil) {
		t.Error("postgres IsMissingTable")
	}
	if !(Postgres{}).IsUniqueViolation(pgDup) || (Postgres{}).IsUniqueViolation(pgMissing) ||
		(Postgres{}).IsUniqueViolation(errors.New("x")) {
		t.Error("postgres IsUniqueViolation")
	}
	if !(SQLite{}).IsMissingTable(errors.New("SQL logic error: no such table: t (1)")) ||
		(SQLite{}).IsMissingTable(errors.New("other")) || (SQLite{}).IsMissingTable(nil) {
		t.Error("sqlite IsMissingTable")
	}
	if (SQLite{}).IsUniqueViolation(errors.New("UNIQUE constraint failed")) || (SQLite{}).IsUniqueViolation(nil) {
		t.Error("sqlite IsUniqueViolation matches by text, want by result code")
	}
}

func TestEncodeBoundIsTheSmallestMillisecondNotBeforeTheInstant(t *testing.T) {
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		at   time.Time
		want int64
	}{
		"whole millisecond":     {base, base.UnixMilli()},
		"one nanosecond after":  {base.Add(time.Nanosecond), base.UnixMilli() + 1},
		"half a millisecond":    {base.Add(500 * time.Microsecond), base.UnixMilli() + 1},
		"just under the next":   {base.Add(time.Millisecond - time.Nanosecond), base.UnixMilli() + 1},
		"the next millisecond":  {base.Add(time.Millisecond), base.UnixMilli() + 1},
		"another zone":          {base.In(time.FixedZone("x", 3600)).Add(time.Microsecond), base.UnixMilli() + 1},
		"before the epoch":      {time.UnixMilli(-5).UTC().Add(time.Microsecond), -4},
		"whole ms before epoch": {time.UnixMilli(-5).UTC(), -5},
	} {
		if got := EncodeBound(c.at); got != c.want {
			t.Errorf("%s: EncodeBound = %d, want %d", name, got, c.want)
		}
		if EncodeTime(c.at) > EncodeBound(c.at) || EncodeBound(c.at)-EncodeTime(c.at) > 1 {
			t.Errorf("%s: bound and floor differ by more than one millisecond", name)
		}
	}
}

func TestOnlyPostgresLocksEventWritesAndListsTheWriters(t *testing.T) {
	if (SQLite{}).LockEventWrite() != "" || (SQLite{}).EventWriters() != "" {
		t.Error("SQLite has one writer and needs no event-write lock")
	}
	lock, writers := (Postgres{}).LockEventWrite(), (Postgres{}).EventWriters()
	if lock != "SELECT pg_advisory_xact_lock_shared(1650751025)" {
		t.Errorf("Postgres LockEventWrite = %q", lock)
	}
	for _, want := range []string{"FROM pg_locks", "locktype = 'advisory'", "granted", "objid = 1650751025", "objsubid = 1", "current_database()"} {
		if !strings.Contains(writers, want) {
			t.Errorf("Postgres EventWriters %q lacks %q", writers, want)
		}
	}
}

func TestOnlyPostgresLocksTheRowsOfATableAlias(t *testing.T) {
	if got := (SQLite{}).LockRow("t"); got != "" {
		t.Errorf("SQLite has one writer and needs no row lock, got %q", got)
	}
	if got := (Postgres{}).LockRow("t"); got != " FOR UPDATE OF t" {
		t.Errorf("Postgres LockRow = %q", got)
	}
}
