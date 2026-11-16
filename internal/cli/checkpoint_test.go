package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

// walFixture opens a migrated SQLite store and writes enough rows to grow its
// -wal file, then returns the store and the -wal path. The writes finish
// before it returns, so no writer is running afterwards.
func walFixture(t *testing.T) (*store.Store, string) {
	t.Helper()
	url := sqliteURL(t)
	st, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer().ExecContext(ctx, `CREATE TABLE wal_fill (id INTEGER PRIMARY KEY, body BLOB)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := st.Writer().ExecContext(ctx, `INSERT INTO wal_fill (body) VALUES (randomblob(8192))`); err != nil {
			t.Fatal(err)
		}
	}
	wal := strings.TrimPrefix(url, "sqlite://") + "-wal"
	if n := walSize(t, wal); n == 0 {
		t.Fatalf("the fixture did not grow %s", wal)
	}
	return st, wal
}

// walSize is the size of the -wal file, 0 when it does not exist.
func walSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

func TestCheckpointerTruncatesTheWALOnItsInterval(t *testing.T) {
	st, wal := walFixture(t)
	stop := startCheckpointer(context.Background(), io.Discard, envWith(map[string]string{"BOARD_SQLITE_CHECKPOINT": "10ms"}), st)
	deadline := time.Now().Add(3 * time.Second)
	for walSize(t, wal) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := walSize(t, wal) // read while the timer is the only thing that has run
	stop()
	if got != 0 {
		t.Fatalf("-wal is %d bytes after the checkpoint interval, want 0", got)
	}
}

func TestCheckpointerWithZeroRunsNoTimer(t *testing.T) {
	st, wal := walFixture(t)
	before := walSize(t, wal)
	stop := startCheckpointer(context.Background(), io.Discard, envWith(map[string]string{"BOARD_SQLITE_CHECKPOINT": "0"}), st)
	time.Sleep(100 * time.Millisecond)
	if got := walSize(t, wal); got != before {
		t.Fatalf("-wal went from %d to %d bytes with the timer off", before, got)
	}
	stop()
}

func TestCheckpointerTruncatesTheWALOnCleanShutdown(t *testing.T) {
	st, wal := walFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	// The timer is off, so only the shutdown can truncate; the store stays open.
	stop := startCheckpointer(ctx, io.Discard, envWith(map[string]string{"BOARD_SQLITE_CHECKPOINT": "0"}), st)
	cancel()
	stop()
	if got := walSize(t, wal); got != 0 {
		t.Fatalf("-wal is %d bytes after a clean shutdown, want 0", got)
	}
}

func TestCheckpointerReportsAFailingCheckpointOnStderr(t *testing.T) {
	st, _ := walFixture(t)
	_ = st.Close()
	var errb syncBuffer
	stop := startCheckpointer(context.Background(), &errb, envWith(map[string]string{"BOARD_SQLITE_CHECKPOINT": "0"}), st)
	stop()
	if !strings.Contains(errb.String(), "vh: checkpoint:") {
		t.Fatalf("stderr %q", errb.String())
	}
}

func TestCheckpointerWithAMalformedIntervalRunsNothing(_ *testing.T) {
	startCheckpointer(context.Background(), io.Discard, envWith(map[string]string{"BOARD_SQLITE_CHECKPOINT": "soon"}), nil)()
}

func TestServeRejectsAMalformedCheckpointInterval(t *testing.T) {
	for _, v := range []string{"soon", "-1m"} {
		code, errs := runServe(t, envWith(map[string]string{"BOARD_SQLITE_CHECKPOINT": v}))
		if code != ExitError || !strings.Contains(errs, "VH_SQLITE_CHECKPOINT") {
			t.Errorf("%q: got code=%d err=%q", v, code, errs)
		}
	}
}
