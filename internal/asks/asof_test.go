package asks

import (
	"testing"
	"time"
)

// An end's commit mark is the real clock read after the clock read that stamps
// its transaction and before its commit returns, to the nanosecond, however far the
// store's own clock is from it; it is read back, and a refused second end
// leaves it alone.
func TestAnEndMarksItsCommitAfterEveryClockReadOfItsTransaction(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		e := newEnv(t)
		k := e.raise(t, New{Task: "aa-1", Question: "q"})
		if k.EndCommitAt != nil {
			t.Fatalf("an open ask has a commit mark %v", k.EndCommitAt)
		}
		var stampRead time.Time
		e.S.Now = func() time.Time {
			if stampRead.IsZero() {
				stampRead = time.Now()
			}
			return t0 // far in the past: the mark does not follow this clock
		}
		var got Ask
		var err error
		before := time.Now()
		if withdraw {
			got, err = e.S.Withdraw(ctx, me, "key:x", k.Ref, "")
		} else {
			got, err = e.S.Answer(ctx, op, "key:x", k.Ref, Reply{Answer: "yes"})
		}
		after := time.Now()
		if err != nil {
			t.Fatal(err)
		}
		mark := got.EndCommitAt
		if mark == nil || mark.Before(stampRead) || mark.Before(before) || mark.After(after) {
			t.Fatalf("withdraw=%v: mark %v, want after the in-transaction clock read %s and by %s", withdraw, mark, stampRead, after)
		}
		read, err := e.S.Get(ctx, k.Ref)
		if err != nil || read.EndCommitAt == nil || !read.EndCommitAt.Equal(*mark) {
			t.Fatalf("withdraw=%v: read back mark %v (%v), want %s", withdraw, read.EndCommitAt, err, mark)
		}
		if _, err := e.S.Answer(ctx, op, "key:x", k.Ref, Reply{Answer: "again"}); err == nil {
			t.Fatalf("withdraw=%v: a second end was allowed", withdraw)
		}
		if again, _ := e.S.Get(ctx, k.Ref); again.EndCommitAt == nil || !again.EndCommitAt.Equal(*mark) {
			t.Errorf("withdraw=%v: the mark moved to %v", withdraw, again.EndCommitAt)
		}
	}
}

// AsOf keeps an ask whose end was marked before the instant, and turns one
// marked at or after it (1ns later, or exactly at it) back into the open ask
// it was, every field of the end cleared; an open ask, and an end without a
// mark, are kept as they are.
func TestAsOfReopensAnAskWhoseEndHadNotCommittedByTheInstant(t *testing.T) {
	at := time.Date(2026, 11, 10, 12, 0, 1, 123_456_789, time.UTC)
	stamp := at.Add(-time.Second)
	mark := func(d time.Duration) *time.Time { m := at.Add(d); return &m }
	answered := Ask{Ref: "A1", State: "answered", Answer: "8080", AnsweredBy: "nik@host", OnBehalfOf: "sam", Channel: "relay",
		AnsweredAt: &stamp, EndedAt: &stamp, PollSeenAt: &stamp}
	withdrawn := Ask{Ref: "A1", State: "withdrawn", WithdrawReason: "moved on", WithdrawnAt: &stamp, EndedAt: &stamp, PollSeenAt: &stamp}
	for _, c := range []struct {
		name   string
		k      Ask
		mark   *time.Time
		reopen bool
	}{
		{"answered 1ns before", answered, mark(-time.Nanosecond), false},
		{"withdrawn 1ns before", withdrawn, mark(-time.Nanosecond), false},
		{"answered exactly at", answered, mark(0), true},
		{"withdrawn exactly at", withdrawn, mark(0), true},
		{"answered 1ns after", answered, mark(time.Nanosecond), true},
		{"withdrawn 75ms after", withdrawn, mark(75 * time.Millisecond), true},
		{"answered without a mark", answered, nil, false},
		{"open", Ask{Ref: "A1", State: "open", PollSeenAt: &stamp}, mark(time.Second), false},
	} {
		k := c.k
		k.EndCommitAt = c.mark
		got := k.AsOf(at)
		switch {
		case !c.reopen && (got.State != k.State || got.EndCommitAt != k.EndCommitAt || got.Answer != k.Answer):
			t.Errorf("%s: got %+v, want it kept", c.name, got)
		case c.reopen && (got.State != "open" || got.Answer != "" || got.AnsweredBy != "" || got.OnBehalfOf != "" ||
			got.Channel != "" || got.WithdrawReason != "" || got.AnsweredAt != nil || got.WithdrawnAt != nil ||
			got.EndedAt != nil || got.EndCommitAt != nil || got.PollSeenAt != &stamp || got.Ref != "A1"):
			t.Errorf("%s: got %+v, want it open as it was", c.name, got)
		}
	}
}
