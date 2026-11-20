package server

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/store"
)

// manyLeasesBoard is a migrated store on dbs with stranded alerts on, holding
// one initiative and seeded tasks, each tagged bulk: every task a live lease due
// an hour after base, every third task an older due lease as well, and every
// second task a later live lease on repo:bulk not due for days. It returns the
// store and base.
func manyLeasesBoard(t *testing.T, dbs databases, seeded int) (*store.Store, time.Time) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(dbs.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	st.SetNotify([]string{store.NotifyStranded})
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return base }
	op := audit.Actor{Handle: "nik@host", Operator: true}
	ini, err := (initiatives.Store{Store: st, Now: clock}).Create(ctx, op, "key:x", initiatives.New{Slug: "bulk", Title: "bulk"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	exec := func(q string, args ...any) {
		if _, err := tx.ExecContext(ctx, st.Dialect().Rebind(q), args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO actors (id, handle, kind, first_seen_at, last_seen_at) VALUES ('agent', 'claude@host', 'agent', 1, 1)`)
	at := store.EncodeTime(base)
	const hour = int64(time.Hour / time.Millisecond)
	lease := func(task string, n int, started, expires int64) {
		id := fmt.Sprintf("%s-l%d", task, n)
		exec(`INSERT INTO leases (id, task_id, actor_id, intent, ttl_seconds, started_at, renewed_at, expires_at) VALUES (?, ?, 'agent', 'bulk', 3600, ?, ?, ?)`,
			id, task, started, started, expires)
		exec(`INSERT INTO lease_resources (lease_id, resource) VALUES (?, 'repo:bulk')`, id)
	}
	for i := 1; i <= seeded; i++ {
		task := fmt.Sprintf("seeded-%05d", i)
		exec(`INSERT INTO tasks (id, initiative_id, seq, title, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, 'agent', ?, ?)`,
			task, ini.ID, i, task, at, at)
		exec(`INSERT INTO task_tags (task_id, tag) VALUES (?, 'bulk')`, task)
		if i%3 == 0 {
			lease(task, 1, at-2*hour, at-hour)
		}
		lease(task, 2, at-hour, at+hour)
		if i%2 == 0 {
			lease(task, 3, at+hour, at+100*hour)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return st, base
}

// column is the one text column of every row of query, in row order.
func column(t *testing.T, st *store.Store, query string) []string {
	t.Helper()
	rows, err := st.Reader().QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheNowReadExpiresAndAnswersPastAThousandLeasesOnEveryLeg(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		// 1200 tasks: 1600 due leases, 600 of them the newest on their task,
		// and 600 live leases in flight, all on one resource
		const seeded = 1200
		st, base := manyLeasesBoard(t, dbs, seeded)
		now := base.Add(48 * time.Hour)
		b, err := (attention.Store{Store: st, Now: func() time.Time { return now }}).Board(context.Background(), attention.Query{}, attention.Window24h)
		if err != nil {
			t.Fatal(err)
		}
		due := column(t, st, `SELECT id FROM leases WHERE expires_at < `+fmt.Sprint(store.EncodeTime(now))+` ORDER BY expires_at, n`)
		if len(due) != seeded+seeded/3 {
			t.Fatalf("%d due leases seeded, want %d", len(due), seeded+seeded/3)
		}
		if expired := column(t, st, `SELECT id FROM leases WHERE state = 'expired' ORDER BY expires_at, n`); !slices.Equal(expired, due) {
			t.Errorf("%d leases expired, want the %d due ones", len(expired), len(due))
		}
		// one event per lease, written in the order the leases fell due
		if events := column(t, st, `SELECT ref_id FROM events WHERE type = 'lease_expired' ORDER BY n`); !slices.Equal(events, due) {
			t.Errorf("%d lease_expired events, not one per due lease in expiry order", len(events))
		}
		alerts := column(t, st, `SELECT e.ref_id FROM notifications n JOIN events e ON e.id = n.event_id WHERE n.kind = 'stranded' ORDER BY e.ref_id`)
		var newest []string
		for i := 1; i <= seeded; i++ {
			if i%2 != 0 {
				newest = append(newest, fmt.Sprintf("seeded-%05d-l2", i))
			}
		}
		if !slices.Equal(alerts, newest) {
			t.Errorf("%d stranded alerts, want one for each of the %d due leases that are the newest on their task", len(alerts), len(newest))
		}
		if len(b.InFlight) != seeded/2 {
			t.Errorf("%d tasks in flight, want %d", len(b.InFlight), seeded/2)
		}
		if c := b.Attention.Conflicts; len(c) != 1 || c[0].Resource != "repo:bulk" || len(c[0].Tasks) != seeded/2 {
			t.Errorf("conflicts = %d, want repo:bulk held on %d tasks", len(c), seeded/2)
		}
		for i := 1; i <= seeded; i++ {
			ref := fmt.Sprintf("bulk-%d", i)
			if m, ok := b.TaskMarks[ref]; !ok || !slices.Equal(m.Tags, []string{"bulk"}) {
				t.Fatalf("task %s marks = %+v (present %v), want its own tag", ref, m, ok)
			}
		}
	})
}
