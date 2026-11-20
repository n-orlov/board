package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// boardOfSize is a database with n tasks in each of two initiatives; a third of
// them have a live lease, a third an ended one and the rest are untouched
// since they were made. It returns the Now read's store and how many statements
// the pools have run.
func boardOfSize(t *testing.T, n int, alerts ...string) (attention.Store, *atomic.Int64, func(time.Duration)) {
	t.Helper()
	ctx := context.Background()
	var count atomic.Int64
	st, err := store.OpenCounting(filepath.Join(t.TempDir(), "b.db"), &count)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if alerts != nil {
		st.SetNotify(alerts)
	}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	op := audit.Actor{Handle: "nik@host", Operator: true}
	agent := audit.Actor{Handle: "claude@host", WayBack: "tmux 1"}
	ini, tk, ls := initiatives.Store{Store: st, Now: clock}, tasks.Store{Store: st, Now: clock}, leases.Store{Store: st, Now: clock}
	for _, slug := range []string{"aa", "bb"} {
		if _, err := ini.Create(ctx, op, "key:x", initiatives.New{Slug: slug, Title: slug, Tags: []string{"web"}}); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= n; i++ {
			if _, err := tk.Create(ctx, op, "key:x", tasks.New{Initiative: slug, Title: fmt.Sprintf("task %d", i), Tags: []string{"own"}}); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Second)
			if i%3 == 0 {
				continue
			}
			l, err := ls.Start(ctx, agent, "key:x", leases.Start{Task: fmt.Sprintf("%s-%d", slug, i), Intent: "work", Resources: []string{"repo:x"}})
			if err != nil {
				t.Fatal(err)
			}
			if i%3 == 2 {
				if _, err := ls.End(ctx, agent, "key:x", l.Lease.Ref, "done for now"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return attention.Store{Store: st, Now: clock}, &count, func(d time.Duration) { now = now.Add(d) }
}

// queriesOfBoard is how many statements one Now read runs on a board of n tasks
// in each of two initiatives, and how many items it answered with.
func queriesOfBoard(t *testing.T, n int, q attention.Query) (int64, int) {
	t.Helper()
	a, count, _ := boardOfSize(t, n)
	before := count.Load()
	b, err := a.Board(context.Background(), q, attention.Window24h)
	if err != nil {
		t.Fatal(err)
	}
	return count.Load() - before, len(b.InFlight) + len(b.Recent)
}

func TestTheNowReadRunsTheSameNumberOfQueriesAtAnyStoreSize(t *testing.T) {
	for name, q := range map[string]attention.Query{
		"no filter":          {},
		"initiative and tag": {Initiatives: []string{"aa"}, Tags: []string{"own"}},
	} {
		small, smallItems := queriesOfBoard(t, 5, q)
		large, largeItems := queriesOfBoard(t, 50, q)
		if largeItems <= smallItems {
			t.Fatalf("%s: the large board answered %d items and the small one %d, so it proves nothing", name, largeItems, smallItems)
		}
		if small != large {
			t.Errorf("%s: %d queries at 5 tasks per initiative but %d at 50: the read is not constant", name, small, large)
		}
		if small == 0 {
			t.Errorf("%s: the read counted no queries, so the counter is not wired", name)
		}
	}
}

// queriesWithDueLeases is queriesOfBoard after the clock has passed the expiry
// of every live lease, so the read meets the due leases and expires them. It
// also says how many lease_expired events the read wrote.
func queriesWithDueLeases(t *testing.T, n int, alerts ...string) (int64, int) {
	t.Helper()
	a, count, advance := boardOfSize(t, n, alerts...)
	advance(48 * time.Hour)
	before := count.Load()
	if _, err := a.Board(context.Background(), attention.Query{}, attention.Window24h); err != nil {
		t.Fatal(err)
	}
	used := count.Load() - before
	var events int
	if err := a.Store.ReaderFor(context.Background()).QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE type = 'lease_expired'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	return used, events
}

func TestTheNowReadRunsTheSameNumberOfQueriesWhenItMeetsDueLeases(t *testing.T) {
	for name, alerts := range map[string][]string{"alerts off": nil, "stranded alerts on": {store.NotifyStranded}} {
		small, smallEvents := queriesWithDueLeases(t, 5, alerts...)
		large, largeEvents := queriesWithDueLeases(t, 50, alerts...)
		if smallEvents == 0 || largeEvents <= smallEvents {
			t.Fatalf("%s: the read expired %d leases at 5 tasks per initiative and %d at 50, so it proves nothing", name, smallEvents, largeEvents)
		}
		if small != large {
			t.Errorf("%s: %d queries with %d due leases but %d with %d: the read is not constant", name, small, smallEvents, large, largeEvents)
		}
	}
}
