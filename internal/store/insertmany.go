package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// InsertMany inserts the rows of table in tx, args holding len(cols) values per
// row, row after row, in one statement however many rows there are: the rows
// travel as a single JSON parameter that the database unpacks into a row set
// (Dialect.InsertSet), so neither the statement count nor the number of bound
// parameters grows with the rows. Each value is a string, an integer or nil, the
// only kinds the board stores; anything else is refused before any write. No
// rows is the same one statement over an empty set, which writes nothing, so a
// caller's statement count does not depend on whether it had rows to write.
func InsertMany(ctx context.Context, tx *sql.Tx, d Dialect, table string, cols []string, args []any) error {
	set, err := rowSet(cols, args)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.InsertSet(table, cols)), set)
	return err
}

// rowSet is args as a JSON array of one object per row, keyed by column.
func rowSet(cols []string, args []any) (string, error) {
	if len(cols) == 0 || len(args)%len(cols) != 0 {
		return "", fmt.Errorf("insert rows: %d values do not fill rows of %d columns", len(args), len(cols))
	}
	rows := make([]map[string]any, 0, len(args)/len(cols))
	for from := 0; from < len(args); from += len(cols) {
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			if !storable(args[from+i]) {
				return "", fmt.Errorf("insert rows: column %s: a %T cannot be stored", col, args[from+i])
			}
			row[col] = args[from+i]
		}
		rows = append(rows, row)
	}
	set, err := json.Marshal(rows)
	return string(set), err
}

// storable reports whether v is a kind of value InsertMany carries unchanged.
func storable(v any) bool {
	switch v.(type) {
	case nil, string, int, int64:
		return true
	}
	return false
}
