package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/n-orlov/village-hall/internal/store"
)

// countingStore is a migrated SQLite store whose pools count every statement.
func countingStore(t *testing.T) (*store.Store, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	st, err := store.OpenCounting(filepath.Join(t.TempDir(), "b.db"), &n)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st, &n
}

var actorCols = []string{"id", "handle", "kind", "display_name", "first_seen_at", "last_seen_at"}

// actorRows is the values of rows actors; every third has no display name, and
// the timestamps are past 2^53, so a value that went through a float shows.
func actorRows(rows int) []any {
	var args []any
	for i := 0; i < rows; i++ {
		var name any = fmt.Sprintf("name %d \"quoted\" <%d> é", i, i)
		if i%3 == 0 {
			name = nil
		}
		args = append(args, fmt.Sprintf("a%05d", i), fmt.Sprintf("h%d", i), "agent", name, int64(1)<<60+int64(i), int64(i))
	}
	return args
}

// insertCounted runs InsertMany into actors in its own committed transaction and
// says how many statements it ran.
func insertCounted(t *testing.T, st *store.Store, n *atomic.Int64, cols []string, args []any) (int64, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	before := n.Load()
	err = store.InsertMany(ctx, tx, st.Dialect(), "actors", cols, args)
	used := n.Load() - before
	if err == nil {
		err = tx.Commit()
	}
	return used, err
}

func TestInsertManyWritesAnyNumberOfRowsInOneStatement(t *testing.T) {
	// 1000 and 2007 straddle the old chunk of 1000 rows per statement; 5000
	// is several of them over
	for _, rows := range []int{1, 2, 999, 1000, 1001, 2007, 5000} {
		t.Run(fmt.Sprint(rows), func(t *testing.T) {
			st, n := countingStore(t)
			used, err := insertCounted(t, st, n, actorCols, actorRows(rows))
			if err != nil {
				t.Fatal(err)
			}
			if used != 1 {
				t.Errorf("%d statements for %d rows, want 1 whatever the row count", used, rows)
			}
			assertActorRows(t, st, rows)
		})
	}
}

// assertActorRows checks every row actorRows made is stored as given, in order.
func assertActorRows(t *testing.T, st *store.Store, rows int) {
	t.Helper()
	got, err := st.Reader().QueryContext(context.Background(), `SELECT id, display_name, first_seen_at, last_seen_at FROM actors ORDER BY n`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.Close() }()
	want, i := actorRows(rows), 0
	for ; got.Next(); i++ {
		var id string
		var name sql.NullString
		var first, last int64
		if err := got.Scan(&id, &name, &first, &last); err != nil {
			t.Fatal(err)
		}
		row := want[i*len(actorCols):]
		wantName, _ := row[3].(string)
		if id != row[0] || name.Valid != (row[3] != nil) || name.String != wantName || first != row[4] || last != row[5] {
			t.Fatalf("row %d = %s %v %d %d, want %v", i, id, name, first, last, row[:len(actorCols)])
		}
	}
	if i != rows {
		t.Errorf("%d rows stored, want %d", i, rows)
	}
}

func TestInsertManyOfNoRowsRunsTheSameOneStatementAndWritesNothing(t *testing.T) {
	// a caller's statement count must not depend on whether it had rows
	st, n := countingStore(t)
	if used, err := insertCounted(t, st, n, actorCols, nil); err != nil || used != 1 {
		t.Errorf("no rows: %d statements, err %v; want the one statement of any row count", used, err)
	}
	assertActorRows(t, st, 0)
}

func TestInsertManyRefusesWhatItCannotCarryBeforeAnyStatement(t *testing.T) {
	for name, args := range map[string][]any{
		"a value of another kind":   {"a", "h", "agent", []byte("x"), int64(1), int64(1)},
		"a float":                   {"a", "h", "agent", nil, 1.5, int64(1)},
		"values that fill no row":   {"a", "h", "agent", nil, int64(1)},
		"a later row of bad values": append(actorRows(2), "z", "hz", "agent", true, int64(1), int64(1)),
	} {
		st, n := countingStore(t)
		used, err := insertCounted(t, st, n, actorCols, args)
		if err == nil || used != 0 {
			t.Errorf("%s: err %v after %d statements, want a refusal before any", name, err, used)
		}
	}
	st, n := countingStore(t)
	if _, err := insertCounted(t, st, n, nil, []any{"a"}); err == nil {
		t.Error("values with no columns were accepted")
	}
}

func TestInsertManyRefusedWritesNoRowOfTheStatement(t *testing.T) {
	st, n := countingStore(t)
	args := append(actorRows(1500), actorRows(1)...) // the last row repeats the first id
	if _, err := insertCounted(t, st, n, actorCols, args); err == nil {
		t.Fatal("two rows with one id were accepted")
	}
	var got int
	if err := st.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM actors`).Scan(&got); err != nil || got != 0 {
		t.Errorf("%d rows stored after a refused statement (err %v)", got, err)
	}
}
