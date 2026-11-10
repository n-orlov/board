package asks

import (
	"testing"
	"time"
)

// An answer or withdrawal records ended_at from the clock read after its
// transaction committed, rounded up to the millisecond, while answered_at and
// withdrawn_at keep the stamp taken inside the transaction; an open ask has
// none, and a refused second end leaves the first one's stamp alone.
func TestAnEndRecordsEndedAtFromTheClockAfterItsCommit(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		e := newEnv(t)
		k := e.raise(t, New{Task: "aa-1", Question: "q"})
		if k.EndedAt != nil {
			t.Fatalf("an open ask has ended_at %v", k.EndedAt)
		}
		stamp := t0.Add(time.Hour + 500*time.Microsecond)
		committed := stamp.Add(2*time.Second + 300*time.Microsecond)
		reads := []time.Time{stamp, committed}
		e.S.Now = func() time.Time {
			now := reads[0]
			if len(reads) > 1 {
				reads = reads[1:]
			}
			return now
		}
		var got Ask
		var err error
		if withdraw {
			got, err = e.S.Withdraw(ctx, me, "key:x", k.Ref, "")
		} else {
			got, err = e.S.Answer(ctx, op, "key:x", k.Ref, Reply{Answer: "yes"})
		}
		if err != nil {
			t.Fatal(err)
		}
		want := committed.Truncate(time.Millisecond).Add(time.Millisecond)
		inTx := got.AnsweredAt
		if withdraw {
			inTx = got.WithdrawnAt
		}
		if got.EndedAt == nil || !got.EndedAt.Equal(want) || inTx == nil || !inTx.Equal(stamp.Truncate(time.Millisecond)) {
			t.Fatalf("withdraw=%v: ended_at %v, stamp %v, want %s and %s", withdraw, got.EndedAt, inTx, want, stamp)
		}
		read, err := e.S.Get(ctx, k.Ref)
		if err != nil || read.EndedAt == nil || !read.EndedAt.Equal(want) {
			t.Fatalf("withdraw=%v: read back ended_at %v (%v)", withdraw, read.EndedAt, err)
		}
		reads = []time.Time{committed.Add(time.Hour)}
		if _, err := e.S.Withdraw(ctx, me, "key:x", k.Ref, ""); err == nil {
			t.Fatalf("withdraw=%v: a second end was allowed", withdraw)
		}
		e.S.stampEnd(ctx, read)
		if again, _ := e.S.Get(ctx, k.Ref); again.EndedAt == nil || !again.EndedAt.Equal(want) {
			t.Errorf("withdraw=%v: ended_at moved to %v", withdraw, again.EndedAt)
		}
	}
}

// A whole-millisecond clock is stored as it is, not a millisecond later.
func TestEndedAtKeepsAWholeMillisecond(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Task: "aa-1", Question: "q"})
	at := t0.Add(time.Hour)
	e.S.Now = func() time.Time { return at }
	got, err := e.S.Answer(ctx, op, "key:x", k.Ref, Reply{Answer: "yes"})
	if err != nil || got.EndedAt == nil || !got.EndedAt.Equal(at) {
		t.Errorf("ended_at %v (%v), want %s", got.EndedAt, err, at)
	}
}

// When the stamp cannot be written the end still stands, without one.
func TestAnEndWhoseStampFailsStandsWithoutOne(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Task: "aa-1", Question: "q"})
	if err := e.S.Store.Close(); err != nil {
		t.Fatal(err)
	}
	if got := e.S.stampEnd(ctx, k); got.EndedAt != nil {
		t.Errorf("ended_at %v after a failed write", got.EndedAt)
	}
}
