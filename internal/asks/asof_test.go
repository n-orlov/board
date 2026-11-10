package asks

import (
	"testing"
	"time"
)

// An end's commit mark is the real clock read only after the end's COMMIT has
// returned (the ask already reads ended to a reader when the clock is read),
// after the clock read that stamps its transaction, to the nanosecond, however
// far the store's own clock is from it; it is read back, and a refused second
// end leaves it alone.
func TestAnEndMarksItsCommitAfterItsCommitReturned(t *testing.T) {
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
		var stateAtMarkRead string
		var markRead time.Time
		e.S.MarkClock = func() time.Time {
			read, err := e.S.Get(ctx, k.Ref)
			if err != nil {
				t.Error(err)
			}
			stateAtMarkRead = read.State
			markRead = time.Now()
			return markRead
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
		if stateAtMarkRead == "" || stateAtMarkRead == "open" {
			t.Fatalf("withdraw=%v: the mark was read while the ask read %q: before the COMMIT returned", withdraw, stateAtMarkRead)
		}
		mark := got.EndCommitAt
		if mark == nil || !mark.Equal(markRead.UTC()) || mark.Before(stampRead) || mark.Before(before) || mark.After(after) {
			t.Fatalf("withdraw=%v: mark %v, want the clock read %s after the COMMIT, and after the in-transaction read %s", withdraw, mark, markRead, stampRead)
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

// No clock read inside the end's transaction is a mark: an end whose
// transaction stamps are far before the instant, but whose mark (read after
// the COMMIT) is delayed past it, is open as of that instant, and the same end
// as of an instant after the mark is kept. An end whose COMMIT returned before
// the instant and whose mark was read at once is kept as of the instant even
// though it is read long after; a read made before the delayed mark is
// written already sees the end committed.
func TestAsOfJudgesAnEndByItsPostCommitMarkNeverItsStamps(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		e := newEnv(t)
		k := e.raise(t, New{Task: "aa-1", Question: "q"})
		until := time.Now().Add(300 * time.Millisecond)
		e.S.Now = func() time.Time { return until.Add(-time.Hour) }
		var seenMid Ask
		e.S.MarkClock = func() time.Time { // delayed: the COMMIT returned before until, the mark is read after it
			seenMid, _ = e.S.Get(ctx, k.Ref)
			time.Sleep(time.Until(until.Add(150 * time.Millisecond)))
			return time.Now()
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
		if seenMid.State == "open" || seenMid.EndCommitAt != nil || time.Now().Before(until) {
			t.Fatalf("withdraw=%v: state %q mark %v at the delayed mark read, want the end committed and unmarked", withdraw, seenMid.State, seenMid.EndCommitAt)
		}
		read, _ := e.S.Get(ctx, k.Ref)
		if as := read.AsOf(until); as.State != "open" || as.EndedAt != nil || as.EndCommitAt != nil || as.Answer != "" {
			t.Errorf("withdraw=%v: a mark delayed past until counted: %+v", withdraw, as)
		}
		if as := read.AsOf(*got.EndCommitAt); as.State == "open" {
			t.Errorf("withdraw=%v: an end as of its own mark is open", withdraw)
		}
		prompt := newEnv(t)
		pk := prompt.raise(t, New{Task: "aa-1", Question: "q"})
		if withdraw {
			_, err = prompt.S.Withdraw(ctx, me, "key:x", pk.Ref, "")
		} else {
			_, err = prompt.S.Answer(ctx, op, "key:x", pk.Ref, Reply{Answer: "yes"})
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		pr, _ := prompt.S.Get(ctx, pk.Ref)
		if as := pr.AsOf(time.Now()); as.State == "open" || as.EndCommitAt == nil {
			t.Errorf("withdraw=%v: an end marked before the instant, read after it, is open: %+v", withdraw, as)
		}
	}
}

// AsOf keeps an ask whose end was marked at or before the instant (exactly at
// it, or 1ns before), and turns one marked after it, or never marked, back into
// the open ask it was, every field of the end cleared; an open ask is kept as
// it is.
func TestAsOfReopensAnAskWhoseEndHadNoMarkByTheInstant(t *testing.T) {
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
		{"answered exactly at", answered, mark(0), false},
		{"withdrawn exactly at", withdrawn, mark(0), false},
		{"answered 1ns after", answered, mark(time.Nanosecond), true},
		{"withdrawn 75ms after", withdrawn, mark(75 * time.Millisecond), true},
		{"answered without a mark", answered, nil, true},
		{"withdrawn without a mark", withdrawn, nil, true},
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
