package asks

import (
	"testing"
	"time"
)

// pollEvents is the number of ask_polling events in the log.
func (e env) pollEvents(t *testing.T) int {
	t.Helper()
	var n int
	err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type = ?`, EventPolling).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Every outcome of MarkPoll: the mark is stored for every ask that exists, and
// the ask_polling event is appended only when the mark makes an open poll ask's
// asker start waiting.
func TestMarkPollStoresTheMarkOnEveryAskAndAppendsTheEventOnlyWhenTheAskerStartsWaiting(t *testing.T) {
	type setup func(t *testing.T, e env) Ask
	raise := func(mode string) setup {
		return func(t *testing.T, e env) Ask { return e.raise(t, New{Question: "q?", Mode: mode}) }
	}
	cases := []struct {
		name  string
		setup setup
		added int
	}{
		{"open poll ask not yet waiting", raise("poll"), 1},
		{"open poll ask already waiting", func(t *testing.T, e env) Ask {
			k := raise("poll")(t, e)
			if _, err := e.S.MarkPoll(ctx, k.ID); err != nil {
				t.Fatal(err)
			}
			e.Clock.now = e.Clock.now.Add(10 * time.Second)
			return k
		}, 0},
		{"open poll ask whose earlier mark has lapsed", func(t *testing.T, e env) Ask {
			k := raise("poll")(t, e)
			if _, err := e.S.MarkPoll(ctx, k.ID); err != nil {
				t.Fatal(err)
			}
			e.Clock.now = e.Clock.now.Add(61 * time.Second)
			return k
		}, 1},
		{"open stop ask", raise("stop"), 0},
		{"answered poll ask", func(t *testing.T, e env) Ask {
			k := raise("poll")(t, e)
			if _, err := e.S.Answer(ctx, op, "key:x", k.Ref, Reply{Answer: "yes"}); err != nil {
				t.Fatal(err)
			}
			return k
		}, 0},
		{"withdrawn poll ask", func(t *testing.T, e env) Ask {
			k := raise("poll")(t, e)
			if _, err := e.S.Withdraw(ctx, me, "key:x", k.Ref, ""); err != nil {
				t.Fatal(err)
			}
			return k
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			k := tc.setup(t, e)
			e.Clock.now = e.Clock.now.Add(time.Second)
			events, counts := e.pollEvents(t), e.counts(t)
			at, err := e.S.MarkPoll(ctx, k.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !at.Equal(e.Clock.now) {
				t.Errorf("MarkPoll returned %v, want the clock %v", at, e.Clock.now)
			}
			got, err := e.S.Get(ctx, k.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if got.PollSeenAt == nil || !got.PollSeenAt.Equal(e.Clock.now) {
				t.Errorf("poll_seen_at = %v, want %v", got.PollSeenAt, e.Clock.now)
			}
			if n := e.pollEvents(t); n != events+tc.added {
				t.Errorf("ask_polling events = %d, want %d", n, events+tc.added)
			}
			if after := e.counts(t); after != [3]int{counts[0], counts[1] + tc.added, counts[2]} {
				t.Errorf("counts = %v after, %v before, want only %d more event(s)", after, counts, tc.added)
			}
		})
	}
}

func TestMarkPollOfAnUnknownIDReturnsNoErrorAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Question: "q?", Mode: "poll"})
	before, events := e.counts(t), e.pollEvents(t)
	e.Clock.now = e.Clock.now.Add(time.Second)
	if _, err := e.S.MarkPoll(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatalf("MarkPoll of an unknown id = %v, want nil", err)
	}
	if after := e.counts(t); after != before || e.pollEvents(t) != events {
		t.Errorf("counts after = %v, want %v", after, before)
	}
	got, err := e.S.Get(ctx, k.Ref)
	if err != nil || got.PollSeenAt != nil {
		t.Errorf("another ask's poll_seen_at = %v (%v), want none", got.PollSeenAt, err)
	}
}
