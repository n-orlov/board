package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/attention"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/tasks"
)

// attentionRig serves asks and attention over one database with the clock the
// test moves. The heartbeat, and with it the recheck, is 100 ms.
func attentionRig(t *testing.T, dbs databases, clock *testClock) (*rig, *tasks.Store) {
	t.Helper()
	st := openStore(t, dbs.New(t))
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := streamSettings{poll: 100 * time.Millisecond, heartbeat: 100 * time.Millisecond, extra: func(st *store.Store) []Option {
		return []Option{WithAsks(&asks.Store{Store: st, Now: clock.Now}), WithAttention(&attention.Store{Store: st, Now: clock.Now})}
	}}
	return startRig(t, st, newToken(t, st), cfg), &tasks.Store{Store: st, Now: clock.Now}
}

// counts reads the data of an `event: attention` message, failing on any other block.
func counts(t *testing.T, block string) AttentionCounts {
	t.Helper()
	event, data, ok := strings.Cut(block, "\n")
	if !ok || event != "event: attention" || !strings.HasPrefix(data, "data: ") || strings.Contains(data, "\n") {
		t.Fatalf("block %q is not an attention message of one event line and one data line", block)
	}
	var c AttentionCounts
	if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &c); err != nil {
		t.Fatalf("data of %q: %v", block, err)
	}
	return c
}

func TestAttentionTopicSendsTheCountsOnConnectAndAfterAChange(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r, _ := attentionRig(t, dbs, newTestClock())
		s := r.openStream("/api/v1/stream?topics=attention")
		if got := counts(t, s.nextMessage()); got != (AttentionCounts{}) {
			t.Errorf("counts on connect = %+v, want all zero", got)
		}
		r.raiseAsk()
		if got, want := counts(t, s.nextMessage()), (AttentionCounts{OpenAsks: 1, Total: 1}); got != want {
			t.Errorf("counts after an ask = %+v, want %+v", got, want)
		}
	})
}

func TestAttentionTopicStaysQuietWhenAChangeLeavesTheCountsEqual(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r, _ := attentionRig(t, dbs, newTestClock())
		s := r.openStream("/api/v1/stream?topics=attention")
		counts(t, s.nextMessage())
		r.createInitiative("quiet-one")
		r.awaitFeed(1)
		s.quiet(600 * time.Millisecond)
		r.raiseAsk()
		if got := counts(t, s.nextMessage()); got.OpenAsks != 1 {
			t.Errorf("counts after the quiet period and an ask = %+v, want one open ask", got)
		}
	})
}

func TestAttentionTopicAloneSendsNoBoardMessage(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r, _ := attentionRig(t, dbs, newTestClock())
		s := r.openStream("/api/v1/stream?topics=attention")
		counts(t, s.nextMessage())
		r.createInitiative("only-attention")
		r.raiseAsk()
		counts(t, s.nextMessage()) // the ask; a `board` message for the initiative or the ask would fail here or below
		r.createInitiative("only-attention-too")
		s.quiet(600 * time.Millisecond)
	})
}

func TestBothTopicsSendTheCountsOnceAndEveryEvent(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r, _ := attentionRig(t, dbs, newTestClock())
		s := r.openStream("/api/v1/stream")
		counts(t, s.nextMessage())
		r.raiseAsk()
		seen := map[string]int{}
		for range 2 { // the ask_raised event and the changed counts, in either order
			seen[strings.SplitN(s.nextMessage(), "\n", 2)[0]]++
		}
		if seen["event: board"] != 1 || seen["event: attention"] != 1 {
			t.Errorf("messages after an ask = %v, want one board and one attention", seen)
		}
	})
}

func TestAttentionTopicSendsAMessageWhenOnlyTheClockMovesAWatchOverdue(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		clock := newTestClock()
		r, ts := attentionRig(t, dbs, clock)
		r.createInitiative("aa")
		cadence := int64(3600)
		owner := audit.Actor{Handle: "codex@host", WayBack: "pid 9"}
		if _, err := ts.Create(context.Background(), owner, "key:x", tasks.New{Initiative: "aa", Title: "watch the build", Kind: "watch", CadenceSeconds: &cadence}); err != nil {
			t.Fatal(err)
		}
		r.awaitFeed(2)
		s := r.openStream("/api/v1/stream?topics=attention")
		if got := counts(t, s.nextMessage()); got.OverdueWatches != 0 {
			t.Fatalf("counts on connect = %+v, want no overdue watch yet", got)
		}
		clock.Advance(90 * time.Minute)
		if got, want := counts(t, s.nextMessage()), (AttentionCounts{OverdueWatches: 1, Total: 1}); got != want {
			t.Errorf("counts after the clock moved = %+v, want %+v", got, want)
		}
	})
}

func TestRecheckIsAtMostThirtySecondsAndFollowsAShorterHeartbeat(t *testing.T) {
	for heartbeat, want := range map[time.Duration]time.Duration{
		25 * time.Second: 25 * time.Second, time.Minute: AttentionRecheck, 100 * time.Millisecond: 100 * time.Millisecond,
	} {
		if got := recheckEvery(heartbeat); got != want {
			t.Errorf("recheckEvery(%v) = %v, want %v", heartbeat, got, want)
		}
	}
	if AttentionRecheck != 30*time.Second {
		t.Errorf("AttentionRecheck = %v, want 30 s", AttentionRecheck)
	}
}
