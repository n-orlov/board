package asks

import (
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// markHolds maps a database file to the chan its end-mark writes wait on.
var markHolds sync.Map // path -> *markHold

type markHold struct {
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

func init() {
	err := sqlite.RegisterScalarFunction("hold_end_commit_mark", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		path, _ := args[0].(string)
		if h, ok := markHolds.Load(path); ok {
			h := h.(*markHold)
			h.once.Do(func() { close(h.reached) })
			<-h.release
		}
		return int64(0), nil
	})
	if err != nil {
		panic(err)
	}
}

// An end's commit mark is written on the store's one writer, in a statement of
// its own after the COMMIT: a write that queues behind it (the long poll's
// marking of the poll is one) returns only once the mark is stored, and a read
// made after that write finds the mark, which is the sampled real-clock time of
// a moment after the end's COMMIT. Answers and withdrawals. The server's wait
// relies on this to take a timely mark that was still being written when its
// deadline passed (SPEC 7.2).
func TestAWriteQueuedBehindAnEndsMarkWriteReturnsOnlyOnceTheMarkIsStored(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		e := newEnv(t)
		k := e.raise(t, New{Task: "aa-1", Question: "q"})
		e.S.Now = time.Now
		var path string
		var seq int
		var name, file string
		rows, err := e.S.Store.Writer().QueryContext(ctx, "PRAGMA database_list")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			if err := rows.Scan(&seq, &name, &file); err != nil {
				t.Fatal(err)
			}
			if name == "main" {
				path = file
			}
		}
		_ = rows.Close()
		h := &markHold{reached: make(chan struct{}), release: make(chan struct{})}
		markHolds.Store(path, h)
		t.Cleanup(func() { markHolds.Delete(path) })
		trigger := fmt.Sprintf(`CREATE TRIGGER hold_mark BEFORE UPDATE OF end_commit_ns ON asks BEGIN SELECT hold_end_commit_mark('%s'); END`, path)
		if _, err := e.S.Store.Writer().ExecContext(ctx, trigger); err != nil {
			t.Fatal(err)
		}
		ended := make(chan Ask, 1)
		go func() {
			var got Ask
			var err error
			if withdraw {
				got, err = e.S.Withdraw(ctx, me, "key:x", k.Ref, "")
			} else {
				got, err = e.S.Answer(ctx, op, "key:x", k.Ref, Reply{Answer: "yes"})
			}
			if err != nil {
				t.Error(err)
			}
			ended <- got
		}()
		<-h.reached
		reachedAt := time.Now()
		mid, err := e.S.Get(ctx, k.Ref)
		if err != nil || mid.State == "open" || mid.EndCommitAt != nil {
			t.Fatalf("withdraw=%v: while the mark write waits the ask reads %q with mark %v (%v), want committed and unmarked", withdraw, mid.State, mid.EndCommitAt, err)
		}
		queued := make(chan time.Time, 1)
		go func() {
			if _, err := e.S.MarkPoll(ctx, k.Ref); err != nil {
				t.Error(err)
			}
			queued <- time.Now()
		}()
		select {
		case <-queued:
			t.Fatalf("withdraw=%v: a write returned while the mark write was still held", withdraw)
		case <-time.After(150 * time.Millisecond):
		}
		close(h.release)
		<-queued
		got, err := e.S.Get(ctx, k.Ref)
		if err != nil || got.EndCommitAt == nil || got.EndCommitAt.After(reachedAt) {
			t.Errorf("withdraw=%v: the mark after the queued write is %v (%v), want one sampled before the write was reached", withdraw, got.EndCommitAt, err)
		}
		if a := <-ended; a.EndCommitAt == nil || !a.EndCommitAt.Equal(*got.EndCommitAt) {
			t.Errorf("withdraw=%v: the end returned mark %v, stored %v", withdraw, a.EndCommitAt, got.EndCommitAt)
		}
	}
}
