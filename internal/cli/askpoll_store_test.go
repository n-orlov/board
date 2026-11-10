package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/store"
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
// millisecond, counts only when it was in effect by the deadline: an answer or
// withdrawal made after a deadline that falls inside a millisecond is exit 3,
// even when the stored stamp is that same millisecond and so not after the
// deadline, and so is one stamped before the deadline whose transaction
// committed after it (late), read after the deadline. One committed by the
// deadline but read only after it (read late) counts, by its ended_at.
func TestPollAskAgainstTheStoreCountsOnlyAnEndMadeByTheDeadline(t *testing.T) {
	ctx := context.Background()
	asker := audit.Actor{Handle: "claude@host"}
	op := audit.Actor{Handle: "nik@host", Operator: true}
	for _, c := range []struct {
		name     string
		deadline time.Time
		endAt    time.Time // the stamp the store's clock gives the end
		how      string    // "late": the end commits after the deadline though stamped before it; "read late": read after it
		withdraw bool
		code     int
	}{
		{"answered 100µs after a sub-ms deadline", at(1_000_500), at(1_000_600), "", false, ExitTimeout},
		{"withdrawn 100µs after a sub-ms deadline", at(1_000_500), at(1_000_600), "", true, ExitTimeout},
		{"answered 1ns after a whole-ms deadline", at(1_000_000), at(1_000_000).Add(time.Nanosecond), "", false, ExitTimeout},
		{"withdrawn 900µs after a whole-ms deadline", at(1_000_000), at(1_000_900), "", true, ExitTimeout},
		{"answered 2ms after the deadline", at(1_000_500), at(1_002_500), "", false, ExitTimeout},
		{"answered 1ms before a sub-ms deadline", at(1_000_500), at(999_500), "", false, ExitOK},
		{"withdrawn 2ms before a sub-ms deadline", at(1_000_500), at(998_500), "", true, ExitConflict},
		{"answered 1ns before a whole-ms deadline", at(1_000_000), at(1_000_000).Add(-time.Nanosecond), "", false, ExitOK},
		{"withdrawn 300µs before a whole-ms deadline", at(1_000_000), at(999_700), "", true, ExitConflict},
		{"answer stamped 1ms before the deadline, committed after it", at(1_000_000), at(999_000), "late", false, ExitTimeout},
		{"withdrawal stamped 500ms before the deadline, committed after it", at(1_000_000), at(500_000), "late", true, ExitTimeout},
		{"answer committed 1ms before the deadline, read 75ms after it", at(1_000_000), at(999_000), "read late", false, ExitOK},
		{"withdrawal committed 300µs before the deadline, read 75ms after it", at(1_000_000), at(999_700), "read late", true, ExitConflict},
		{"answer committed 1.2ms before a sub-ms deadline, read after it", at(1_000_500), at(999_300), "read late", false, ExitOK},
		{"withdrawal committed 1ns after a whole-ms deadline, read after it", at(1_000_000), at(1_000_000).Add(time.Nanosecond), "read late", true, ExitTimeout},
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
			// the store's clock: each read takes the next of clocks, the last one stays
			clocks := []time.Time{at(0).Add(-time.Minute)}
			s := asks.Store{Store: st, Now: func() time.Time {
				now := clocks[0]
				if len(clocks) > 1 {
					clocks = clocks[1:]
				}
				return now
			}}
			k, err := s.Raise(ctx, asker, "key:x", asks.New{Question: "Which port?", Mode: "poll"})
			if err != nil {
				t.Fatal(err)
			}
			clock := &pollClock{t: at(0)}
			fetch := func(_ int, _ time.Duration) (askView, json.RawMessage, error) {
				readAt, committedAt := c.deadline, c.endAt // an end in effect before the deadline is read by it
				if c.how == "late" || c.endAt.After(c.deadline) {
					if got, err := s.Get(ctx, k.Ref); err != nil || got.State != "open" {
						t.Fatalf("at the deadline the ask is %q (%v), want open", got.State, err)
					}
				}
				if c.how != "" || c.endAt.After(c.deadline) {
					readAt = c.deadline.Add(75 * time.Millisecond) // the end is read after the deadline
				}
				if c.how == "late" {
					committedAt = readAt // and commits after it too
				}
				clocks = []time.Time{c.endAt, committedAt} // the stamp in the transaction, then the clock after the commit
				if c.withdraw {
					_, err = s.Withdraw(ctx, asker, "key:x", k.Ref, "")
				} else {
					_, err = s.Answer(ctx, op, "key:x", k.Ref, asks.Reply{Answer: "8080"})
				}
				if err != nil {
					t.Fatal(err)
				}
				clock.t = readAt
				got, err := s.Get(ctx, k.Ref)
				return storedAskView(got), nil, err
			}
			a, _, err := pollAsk(storedAskView(k), nil, c.deadline, clock.now, fetch)
			if err != nil || pollExit(a.State) != c.code {
				t.Errorf("got %q exit %d (%v), want exit %d", a.State, pollExit(a.State), err, c.code)
			}
		})
	}
}

// at is 2026-11-09 20:30:00 plus us microseconds.
func at(us int) time.Time {
	return time.Date(2026, 11, 9, 20, 30, 0, 0, time.UTC).Add(time.Duration(us) * time.Microsecond)
}
