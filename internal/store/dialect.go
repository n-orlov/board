package store

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"
)

// Dialect is the small set of differences between the supported databases
// that hand-written portable SQL still has to care about (SPEC 12.2). Queries
// are written once with `?` placeholders and passed through Rebind.
type Dialect interface {
	// Name is "sqlite" or "postgres".
	Name() string
	// GooseDialect is the goose dialect that applies this database's migrations.
	GooseDialect() goose.Dialect
	// Placeholder is the 1-based positional parameter marker.
	Placeholder(i int) string
	// Rebind rewrites `?` markers outside string literals into Placeholder form.
	Rebind(query string) string
	// Insert builds INSERT INTO table (cols) VALUES (?, ...) with `?` markers.
	Insert(table string, cols []string) string
	// InsertReturning is Insert followed by RETURNING returning.
	InsertReturning(table string, cols []string, returning ...string) string
	// Upsert builds an insert that, on a conflict over the conflict columns,
	// updates the named columns from the proposed row.
	Upsert(table string, cols, conflict, update []string) string
	// PartialIndex builds CREATE [UNIQUE] INDEX ... WHERE where.
	PartialIndex(name, table string, cols []string, where string, unique bool) string
	// EncodeBool is the bound value for a boolean column.
	EncodeBool(v bool) any
	// SupportsListenNotify reports whether LISTEN/NOTIFY exists.
	SupportsListenNotify() bool
	// IsMissingTable reports whether err says a table does not exist.
	IsMissingTable(err error) bool
	// IsUniqueViolation reports whether err is a unique or primary key violation.
	IsUniqueViolation(err error) bool
}

// EncodeTime is the stored form of a timestamp in every dialect: UTC
// milliseconds since the Unix epoch.
func EncodeTime(t time.Time) int64 { return t.UTC().UnixMilli() }

// DecodeTime is the inverse of EncodeTime.
func DecodeTime(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// SQLite is the Dialect for modernc.org/sqlite.
type SQLite struct{}

// Postgres is the Dialect for pgx.
type Postgres struct{}

// Name implements Dialect.
func (SQLite) Name() string { return "sqlite" }

// Name implements Dialect.
func (Postgres) Name() string { return "postgres" }

// GooseDialect implements Dialect.
func (SQLite) GooseDialect() goose.Dialect { return goose.DialectSQLite3 }

// GooseDialect implements Dialect.
func (Postgres) GooseDialect() goose.Dialect { return goose.DialectPostgres }

// Placeholder implements Dialect.
func (SQLite) Placeholder(int) string { return "?" }

// Placeholder implements Dialect.
func (Postgres) Placeholder(i int) string { return "$" + strconv.Itoa(i) }

// Rebind implements Dialect.
func (SQLite) Rebind(q string) string { return q }

// Rebind implements Dialect.
func (p Postgres) Rebind(q string) string {
	var b strings.Builder
	n, quoted := 0, false
	for i := 0; i < len(q); i++ {
		switch c := q[i]; {
		case c == '\'':
			quoted = !quoted
			b.WriteByte(c)
		case c == '?' && !quoted:
			n++
			b.WriteString(p.Placeholder(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func insert(table string, cols []string) string {
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ", "), marks)
}

func returningClause(returning []string) string {
	if len(returning) == 0 {
		return ""
	}
	return " RETURNING " + strings.Join(returning, ", ")
}

func upsert(table string, cols, conflict, update []string) string {
	sets := make([]string, len(update))
	for i, c := range update {
		sets[i] = c + " = excluded." + c
	}
	action := "DO NOTHING"
	if len(sets) > 0 {
		action = "DO UPDATE SET " + strings.Join(sets, ", ")
	}
	return insert(table, cols) + " ON CONFLICT (" + strings.Join(conflict, ", ") + ") " + action
}

func partialIndex(name, table string, cols []string, where string, unique bool) string {
	kind := "INDEX"
	if unique {
		kind = "UNIQUE INDEX"
	}
	return fmt.Sprintf("CREATE %s %s ON %s (%s) WHERE %s", kind, name, table, strings.Join(cols, ", "), where)
}

// Insert implements Dialect.
func (SQLite) Insert(table string, cols []string) string { return insert(table, cols) }

// Insert implements Dialect.
func (Postgres) Insert(table string, cols []string) string { return insert(table, cols) }

// InsertReturning implements Dialect (SQLite 3.35+ supports RETURNING).
func (SQLite) InsertReturning(table string, cols []string, returning ...string) string {
	return insert(table, cols) + returningClause(returning)
}

// InsertReturning implements Dialect.
func (Postgres) InsertReturning(table string, cols []string, returning ...string) string {
	return insert(table, cols) + returningClause(returning)
}

// Upsert implements Dialect.
func (SQLite) Upsert(table string, cols, conflict, update []string) string {
	return upsert(table, cols, conflict, update)
}

// Upsert implements Dialect.
func (Postgres) Upsert(table string, cols, conflict, update []string) string {
	return upsert(table, cols, conflict, update)
}

// PartialIndex implements Dialect.
func (SQLite) PartialIndex(name, table string, cols []string, where string, unique bool) string {
	return partialIndex(name, table, cols, where, unique)
}

// PartialIndex implements Dialect.
func (Postgres) PartialIndex(name, table string, cols []string, where string, unique bool) string {
	return partialIndex(name, table, cols, where, unique)
}

// EncodeBool implements Dialect: SQLite has no boolean type and stores 0 or 1.
func (SQLite) EncodeBool(v bool) any {
	if v {
		return 1
	}
	return 0
}

// EncodeBool implements Dialect.
func (Postgres) EncodeBool(v bool) any { return v }

// SupportsListenNotify implements Dialect.
func (SQLite) SupportsListenNotify() bool { return false }

// SupportsListenNotify implements Dialect.
func (Postgres) SupportsListenNotify() bool { return true }

// IsMissingTable implements Dialect.
func (SQLite) IsMissingTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

// IsMissingTable implements Dialect (SQLSTATE 42P01, undefined_table).
func (Postgres) IsMissingTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// SQLite result codes for a UNIQUE and a PRIMARY KEY constraint failure.
const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// IsUniqueViolation implements Dialect.
func (SQLite) IsUniqueViolation(err error) bool {
	var sErr *sqlite.Error
	if !errors.As(err, &sErr) {
		return false
	}
	return sErr.Code() == sqliteConstraintUnique || sErr.Code() == sqliteConstraintPrimaryKey
}

// IsUniqueViolation implements Dialect (SQLSTATE 23505, unique_violation).
func (Postgres) IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
