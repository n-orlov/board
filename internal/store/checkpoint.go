package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultCheckpointInterval is how often `board serve` checkpoints a SQLite
// write-ahead log when BOARD_SQLITE_CHECKPOINT is unset.
const DefaultCheckpointInterval = 10 * time.Minute

// ParseCheckpointInterval reads a BOARD_SQLITE_CHECKPOINT value: empty is
// DefaultCheckpointInterval, `0` (`0s`, `off`) is zero, which turns the timer
// off, and anything else must be a Go duration, not negative.
func ParseCheckpointInterval(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultCheckpointInterval, nil
	}
	if v == "0" || strings.EqualFold(v, "off") {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("BOARD_SQLITE_CHECKPOINT=%q: want a duration such as 10m, or 0 to turn it off", v)
	}
	return d, nil
}

// Checkpoint folds the SQLite write-ahead log into the database file and
// truncates it (PRAGMA wal_checkpoint(TRUNCATE)). On Postgres it does nothing.
// A reader that holds the log open makes the pragma report busy after the
// busy timeout; the log is then left for the next checkpoint, which is no error.
func (s *Store) Checkpoint(ctx context.Context) error {
	if s.dialect.Name() != "sqlite" {
		return nil
	}
	var busy, logFrames, checkpointed int
	if err := s.writer.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("checkpoint the write-ahead log: %w", err)
	}
	return nil
}
