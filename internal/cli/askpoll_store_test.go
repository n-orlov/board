package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/store"
)

// storedAskView is the ask as the server returns it: its stamps formatted as
// the JSON encoding of a time.Time does.
func storedAskView(k asks.Ask) askView {
	v := askView{Ref: k.Ref, State: k.State, Answer: k.Answer}
	if k.AnsweredAt != nil {
		v.AnsweredAt = k.AnsweredAt.Format(time.RFC3339Nano)
	}
	if k.WithdrawnAt != nil {
		v.WithdrawnAt = k.WithdrawnAt.Format(time.RFC3339Nano)
	}
	if k.EndedAt != nil {
		v.EndedAt = k.EndedAt.Format(time.RFC3339Nano)
	}
	return v
}

// An end persisted by the real store, whose stamps are floored to the
// millisecond, counts only when it was committed by the deadline: the fetch
// plays the server, reading the ask at readAt (the deadline itself, or a
// moment later when its timer is late), and the end commits at its own
// instant, before or after that read. An end committed after the deadline is
// exit 3, by the server's read when that read is on time, and by its stamp,
// which then is at or after the deadline, when that read is late. One
// committed before the deadline is exit 0 or 6 however late it is read and
// however late the answer arrives, even inside the deadline's own millisecond,
// where its stamp cannot say on which side of the deadline it lies.
func TestPollAskAgainstTheStoreCountsOnlyAnEndCommittedByTheDeadline(t *testing.T) {
	ctx := context.Background()
	asker := audit.Actor{Handle: "claude@host"}
	op := audit.Actor{Handle: "nik@host", Operator: true}
	const late = 5 * time.Millisecond // a server read made this long after the deadline
	for _, c := range []struct {
		name     string
		deadline time.Time
		endAt    time.Time     // the stamp the store's clock gives the end, and its commit
		readLate time.Duration // how long after the deadline the server reads the ask
		withdraw bool
		code     int
	}{
		{"answered 100µs after a sub-ms deadline", at(1_000_500), at(1_000_600), 0, false, ExitTimeout},
		{"withdrawn 100µs after a sub-ms deadline", at(1_000_500), at(1_000_600), 0, true, ExitTimeout},
		{"answered 200µs after a sub-ms deadline", at(1_000_123), at(1_000_323), 0, false, ExitTimeout},
		{"answered 1ns after a whole-ms deadline", at(1_000_000), at(1_000_000).Add(time.Nanosecond), late, false, ExitTimeout},
		{"withdrawn 900µs after a whole-ms deadline", at(1_000_000), at(1_000_900), late, true, ExitTimeout},
		{"answered 2ms after the deadline", at(1_000_500), at(1_002_500), late, false, ExitTimeout},
		{"withdrawn 1ms after a sub-ms deadline", at(1_000_500), at(1_001_500), late, true, ExitTimeout},
		{"answered 100µs before a sub-ms deadline, same millisecond", at(1_000_500), at(1_000_400), 0, false, ExitOK},
		{"withdrawn 100µs before a sub-ms deadline, same millisecond", at(1_000_500), at(1_000_400), 0, true, ExitConflict},
		{"answered 50µs before a sub-ms deadline, read late", at(1_000_123), at(1_000_073), late, false, ExitOK},
		{"withdrawn 50µs before a sub-ms deadline, read late", at(1_000_123), at(1_000_073), late, true, ExitConflict},
		{"answered 1ms before a sub-ms deadline", at(1_000_500), at(999_500), late, false, ExitOK},
		{"withdrawn 2ms before a sub-ms deadline", at(1_000_500), at(998_500), late, true, ExitConflict},
		{"answered 1ns before a whole-ms deadline", at(1_000_000), at(1_000_000).Add(-time.Nanosecond), late, false, ExitOK},
		{"withdrawn 300µs before a whole-ms deadline", at(1_000_000), at(999_700), late, true, ExitConflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			if err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			now := at(0).Add(-time.Minute)
			s := asks.Store{Store: st, Now: func() time.Time { return now }}
			k, err := s.Raise(ctx, asker, "key:x", asks.New{Question: "Which port?", Mode: "poll"})
			if err != nil {
				t.Fatal(err)
			}
			end := func() {
				now = c.endAt
				var err error
				if c.withdraw {
					_, err = s.Withdraw(ctx, asker, "key:x", k.Ref, "")
				} else {
					_, err = s.Answer(ctx, op, "key:x", k.Ref, asks.Reply{Answer: "8080"})
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			clock := &pollClock{t: at(0)}
			fetch := func(_ int, until time.Time) read {
				if !until.Equal(c.deadline) {
					t.Errorf("until = %s, want the deadline %s", until.Format(time.RFC3339Nano), c.deadline.Format(time.RFC3339Nano))
				}
				readAt := until.Add(c.readLate)
				committedFirst := !c.endAt.After(readAt)
				if committedFirst {
					end()
				}
				got, err := s.Get(ctx, k.Ref)
				if !committedFirst {
					end() // commits after the server's read
				}
				clock.t = c.deadline.Add(75 * time.Millisecond) // the answer arrives after the deadline
				return read{a: storedAskView(got), err: err}
			}
			r := pollAsk(read{a: storedAskView(k)}, c.deadline, clock.now, fetch)
			if r.err != nil || pollExit(r.a.State) != c.code {
				t.Errorf("got %q exit %d (%v), want exit %d", r.a.State, pollExit(r.a.State), r.err, c.code)
			}
		})
	}
}

// at is 2026-11-09 20:30:00 plus us microseconds.
func at(us int) time.Time {
	return time.Date(2026, 11, 9, 20, 30, 0, 0, time.UTC).Add(time.Duration(us) * time.Microsecond)
}
