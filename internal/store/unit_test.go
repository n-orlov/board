package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// unitStore is a migrated store with a table t of unique values.
func unitStore(t *testing.T) *Store {
	t.Helper()
	s := open(t)
	if _, err := s.Writer().Exec(`CREATE TABLE t (v TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	return s
}

func put(ctx context.Context, t *testing.T, q Querier, v string) {
	t.Helper()
	if _, err := q.ExecContext(ctx, `INSERT INTO t (v) VALUES (?)`, v); err != nil {
		t.Fatalf("insert %s: %v", v, err)
	}
}

func rows(ctx context.Context, t *testing.T, q Querier) int {
	t.Helper()
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func beginUnit(t *testing.T, s *Store) (context.Context, *Unit) {
	t.Helper()
	ctx, u, err := s.BeginUnit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ctx, u
}

func TestAUnitCommitsWhatItsStatementsAndTransactionsWroteTogether(t *testing.T) {
	s := unitStore(t)
	ctx, u := beginUnit(t, s)
	put(ctx, t, s.WriterFor(ctx), "a")
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	put(ctx, t, tx, "b")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var order []string
	s.AfterCommit(ctx, func(hook context.Context) {
		order = append(order, "after")
		if s.unitOf(hook) != nil || rows(hook, t, s.Reader()) != 2 {
			t.Error("the hook runs under the unit or before its writes are visible")
		}
	})
	if rows(ctx, t, s.ReaderFor(ctx)) != 2 || rows(context.Background(), t, s.Reader()) != 0 || len(order) != 0 {
		t.Fatalf("before the commit the unit sees %d rows, the pool %d, hooks %v; want 2, 0 and none",
			rows(ctx, t, s.ReaderFor(ctx)), rows(context.Background(), t, s.Reader()), order)
	}
	if err := u.Commit(); err != nil {
		t.Fatal(err)
	}
	if rows(context.Background(), t, s.Reader()) != 2 || len(order) != 1 {
		t.Errorf("after the commit the pool sees %d rows and hooks ran %v, want 2 and once", rows(context.Background(), t, s.Reader()), order)
	}
}

func TestAUnitThatRollsBackLeavesNothingAndRunsNoHook(t *testing.T) {
	s := unitStore(t)
	ctx, u := beginUnit(t, s)
	put(ctx, t, s.WriterFor(ctx), "a")
	ran := false
	s.AfterCommit(ctx, func(context.Context) { ran = true })
	if err := u.Rollback(); err != nil {
		t.Fatal(err)
	}
	if rows(context.Background(), t, s.Reader()) != 0 || ran {
		t.Errorf("after the rollback %d rows, hook ran %v, want none and no", rows(context.Background(), t, s.Reader()), ran)
	}
}

func TestATransactionRolledBackInsideAUnitUndoesOnlyItsOwnWrites(t *testing.T) {
	s := unitStore(t)
	ctx, u := beginUnit(t, s)
	put(ctx, t, s.WriterFor(ctx), "kept")
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	put(ctx, t, tx, "undone")
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("commit after rollback = %v, want sql.ErrTxDone", err)
	}
	if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("second rollback = %v, want sql.ErrTxDone", err)
	}
	done, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := done.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := done.Rollback(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rollback after commit = %v, want sql.ErrTxDone", err)
	}
	if err := u.Commit(); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := s.Reader().QueryRow(`SELECT v FROM t`).Scan(&v); err != nil || v != "kept" || rows(context.Background(), t, s.Reader()) != 1 {
		t.Errorf("committed rows: %q (%v), want only kept", v, err)
	}
}

func TestAFailedStatementInsideAUnitUndoesOnlyItselfAndTheUnitStillCommits(t *testing.T) {
	s := unitStore(t)
	ctx, u := beginUnit(t, s)
	put(ctx, t, s.WriterFor(ctx), "a")
	if _, err := s.WriterFor(ctx).ExecContext(ctx, `INSERT INTO t (v) VALUES ('b'), ('a')`); err == nil {
		t.Fatal("a duplicate value was accepted")
	}
	put(ctx, t, s.WriterFor(ctx), "c")
	if err := u.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := rows(context.Background(), t, s.Reader()); n != 2 {
		t.Errorf("%d rows committed, want a and c only", n)
	}
}

func TestAnEndedUnitSendsItsContextBackToThePools(t *testing.T) {
	s := unitStore(t)
	other := unitStore(t)
	ctx, u := beginUnit(t, s)
	if other.WriterFor(ctx) != other.writer || other.ReaderFor(ctx) != other.reader {
		t.Error("another store's statements run in this store's unit")
	}
	if err := u.Commit(); err != nil {
		t.Fatal(err)
	}
	if s.WriterFor(ctx) != s.writer || s.ReaderFor(ctx) != s.reader {
		t.Error("an ended unit still takes statements")
	}
	if err := u.Commit(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("second commit = %v, want sql.ErrTxDone", err)
	}
	if err := u.Rollback(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rollback after commit = %v, want sql.ErrTxDone", err)
	}
	ran := false
	s.AfterCommit(ctx, func(context.Context) { ran = true })
	if !ran {
		t.Error("a hook without an open unit did not run at once")
	}
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	put(ctx, t, tx, "plain")
	if err := tx.Commit(); err != nil || rows(context.Background(), t, s.Reader()) != 1 {
		t.Errorf("a plain transaction committed %v with %d rows", err, rows(context.Background(), t, s.Reader()))
	}
	if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rollback of a committed plain transaction = %v, want sql.ErrTxDone", err)
	}
}

// broken is a unit whose transaction has ended under it, so every statement in
// it fails.
func broken(t *testing.T, s *Store) (context.Context, *Unit) {
	t.Helper()
	ctx, u := beginUnit(t, s)
	if err := u.tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	return ctx, u
}

func TestEveryStepOfAUnitWhoseTransactionIsGoneFails(t *testing.T) {
	s := unitStore(t)
	ctx, u := broken(t, s)
	if _, err := s.Begin(ctx); err == nil {
		t.Error("a savepoint was taken in a unit whose transaction is gone")
	}
	if _, err := s.WriterFor(ctx).ExecContext(ctx, `INSERT INTO t (v) VALUES ('a')`); err == nil {
		t.Error("a statement ran in a unit whose transaction is gone")
	}
	if err := u.Commit(); err == nil {
		t.Error("a unit whose transaction is gone committed")
	}
	if _, again, err := s.BeginUnit(context.Background()); err != nil || again.Rollback() != nil {
		t.Fatalf("a new unit after the broken one: %v", err)
	}
	closed := unitStore(t)
	_ = closed.Close()
	if _, _, err := closed.BeginUnit(context.Background()); err == nil {
		t.Error("a unit began on a closed store")
	}
	if _, err := closed.Begin(context.Background()); err == nil {
		t.Error("a transaction began on a closed store")
	}
}

func TestATransactionInsideAUnitWhoseTransactionIsGoneCannotEnd(t *testing.T) {
	s := unitStore(t)
	ctx, u := beginUnit(t, s)
	committed, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolled, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = u.tx.Rollback()
	if err := committed.Commit(); err == nil {
		t.Error("a savepoint was released in a transaction that is gone")
	}
	if err := rolled.Rollback(); err == nil {
		t.Error("a savepoint was rolled back to in a transaction that is gone")
	}
}
