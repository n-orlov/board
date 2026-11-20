package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/n-orlov/village-hall/internal/store"
)

func TestInsertManyWritesEveryRowInOneStatementPerChunk(t *testing.T) {
	ctx := context.Background()
	for _, rows := range []int{0, 1, store.InsertManyRows, store.InsertManyRows + 1, 2*store.InsertManyRows + 7} {
		t.Run(fmt.Sprint(rows), func(t *testing.T) {
			var count struct{ n atomic.Int64 }
			st, err := store.OpenCounting(filepath.Join(t.TempDir(), "b.db"), &count.n)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			if err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			cols := []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"}
			var args []any
			for i := 0; i < rows; i++ {
				args = append(args, fmt.Sprintf("a%d", i), fmt.Sprintf("h%d", i), "agent", int64(1), int64(1))
			}
			tx, err := st.Writer().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			before := count.n.Load()
			if err := store.InsertMany(ctx, tx, st.Dialect(), "actors", cols, args); err != nil {
				t.Fatal(err)
			}
			used := count.n.Load() - before
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var got int
			if err := st.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM actors`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := (rows + store.InsertManyRows - 1) / store.InsertManyRows; got != rows || used != int64(want) {
				t.Errorf("%d rows stored and %d statements for %d rows, want %d statements", got, used, rows, want)
			}
		})
	}
}

func TestInsertManyRefusedWritesNoRowOfTheStatement(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	cols := []string{"id", "handle", "kind", "first_seen_at", "last_seen_at"}
	args := []any{"a", "h", "agent", int64(1), int64(1), "a", "h2", "agent", int64(1), int64(1)}
	if err := store.InsertMany(ctx, tx, st.Dialect(), "actors", cols, args); err == nil {
		t.Fatal("two rows with one id were accepted")
	}
}
