package store

import (
	"context"
	"database/sql"
)

// InsertManyRows is how many rows one statement of InsertMany carries: far below
// both databases' bound-parameter limits at any column count the board has.
const InsertManyRows = 1000

// InsertMany inserts the rows of table in tx, args holding len(cols) values per
// row, row after row. It runs one multi-row INSERT per InsertManyRows rows, so
// the number of statements does not grow with the number of rows the way one
// INSERT per row does.
func InsertMany(ctx context.Context, tx *sql.Tx, d Dialect, table string, cols []string, args []any) error {
	per := len(cols)
	for from := 0; from < len(args); from += InsertManyRows * per {
		to := min(from+InsertManyRows*per, len(args))
		if _, err := tx.ExecContext(ctx, d.Rebind(InsertRows(table, cols, (to-from)/per)), args[from:to]...); err != nil {
			return err
		}
	}
	return nil
}
