package perf

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/events"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/store"
)

var seedNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func seeded(t *testing.T, plan SeedPlan) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	plan.Now = seedNow
	if err := Seed(ctx, st.Writer(), plan); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return st
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestSeedWritesTheRowsThePlanNames(t *testing.T) {
	plan := SeedPlan{Events: 250, Notes: 50, Leases: 30}
	db := seeded(t, plan).Reader()
	for query, want := range map[string]int{
		`SELECT count(*) FROM events`:                                                250,
		`SELECT count(*) FROM events WHERE type = 'note_posted'`:                     50,
		`SELECT count(*) FROM notes`:                                                 50,
		`SELECT count(*) FROM leases WHERE state = 'live'`:                           30,
		`SELECT count(*) FROM lease_resources`:                                       30,
		`SELECT count(*) FROM actors`:                                                actorCount,
		`SELECT count(*) FROM initiatives`:                                           initiativeCount,
		`SELECT count(*) FROM tasks`:                                                 30 + doneTasks + openTasks + watchTasks,
		`SELECT count(*) FROM tasks WHERE status = 'done'`:                           doneTasks,
		`SELECT count(*) FROM tasks WHERE kind = 'watch'`:                            watchTasks,
		`SELECT count(*) FROM notes WHERE task_id IS NULL AND initiative_id IS NULL`: 10,
		`SELECT count(*) FROM notes WHERE mentions LIKE '%perf-agent-00%'`:           5,
	} {
		if got := count(t, db, query); got != want {
			t.Errorf("%s = %d, want %d", query, got, want)
		}
	}
}

func TestEveryNoteHasItsPostedEventAndEveryReferenceResolves(t *testing.T) {
	db := seeded(t, SeedPlan{Events: 300, Notes: 60, Leases: 20}).Reader()
	if n := count(t, db, `SELECT count(*) FROM notes n WHERE NOT EXISTS (SELECT 1 FROM events e WHERE e.type = 'note_posted' AND e.ref_id = n.id)`); n != 0 {
		t.Errorf("%d notes have no note_posted event", n)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Error("foreign_key_check found a dangling reference")
	}
	// Task sequence numbers are contiguous and the initiatives' counters follow them.
	if n := count(t, db, `SELECT count(*) FROM initiatives i WHERE i.next_task_seq <> (SELECT count(*) FROM tasks t WHERE t.initiative_id = i.id) + 1`); n != 0 {
		t.Errorf("%d initiatives have a wrong next_task_seq", n)
	}
	if n := count(t, db, `SELECT count(*) FROM tasks WHERE seq > (SELECT next_task_seq FROM initiatives WHERE id = initiative_id) - 1`); n != 0 {
		t.Errorf("%d tasks are past their initiative's counter", n)
	}
}

func TestEventsRunInOrderToTheEndOfTheSeed(t *testing.T) {
	db := seeded(t, SeedPlan{Events: 200, Notes: 20, Leases: 10}).Reader()
	var out int
	err := db.QueryRow(`SELECT count(*) FROM events a JOIN events b ON b.n = a.n + 1 WHERE b.id <= a.id OR b.at < a.at`).Scan(&out)
	if err != nil || out != 0 {
		t.Fatalf("events out of id or time order: %d, %v", out, err)
	}
	var last int64
	if err := db.QueryRow(`SELECT max(at) FROM events`).Scan(&last); err != nil || last >= seedNow.UnixMilli() {
		t.Fatalf("latest event at %d is not before the seed's end %d (%v)", last, seedNow.UnixMilli(), err)
	}
	var first int64
	if err := db.QueryRow(`SELECT min(at) FROM events`).Scan(&first); err != nil || first < seedNow.Add(-SeedSpan).UnixMilli() {
		t.Fatalf("earliest event at %d is before the span (%v)", first, err)
	}
}

func TestTheProductReadsTheSeed(t *testing.T) {
	ctx := context.Background()
	st := seeded(t, SeedPlan{Events: 400, Notes: 40, Leases: 25})
	ls := leases.Store{Store: st, Now: func() time.Time { return seedNow.Add(time.Minute) }}
	now, err := ls.CheckIn(ctx, ActorHandle, nil)
	if err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if len(now.Mine) != ownResources || len(now.Others) != 25-ownResources {
		t.Errorf("the harness actor holds %d leases and others %d, want %d and %d", len(now.Mine), len(now.Others), ownResources, 25-ownResources)
	}
	if len(now.Conflicts) != 0 {
		t.Errorf("the harness actor's check-in has conflicts (vh now would exit 7): %+v", now.Conflicts)
	}
	page, err := events.Store{Store: st}.List(ctx, events.Query{Limit: 1000})
	if err != nil || len(page.Items) != 400 {
		t.Fatalf("listing events = %d items, %v", len(page.Items), err)
	}
}

func TestSeedRefusesAPlanItCannotWrite(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for name, plan := range map[string]SeedPlan{
		"no events":      {Events: 0, Leases: 1, Now: seedNow},
		"no leases":      {Events: 10, Leases: 0, Now: seedNow},
		"negative notes": {Events: 10, Notes: -1, Leases: 1, Now: seedNow},
		"too many notes": {Events: 10, Notes: 11, Leases: 1, Now: seedNow},
		"no end time":    {Events: 10, Leases: 1},
	} {
		if err := Seed(ctx, st.Writer(), plan); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSeedFailsOnAStoreThatIsNotMigrated(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := Seed(context.Background(), st.Writer(), SeedPlan{Events: 10, Leases: 1, Now: seedNow}); err == nil {
		t.Fatal("seeding an empty database file should fail")
	}
}

func TestNoNotesIsAValidPlan(t *testing.T) {
	db := seeded(t, SeedPlan{Events: 50, Notes: 0, Leases: 3}).Reader()
	if n := count(t, db, `SELECT count(*) FROM notes`); n != 0 {
		t.Errorf("%d notes", n)
	}
	if n := count(t, db, `SELECT count(*) FROM events`); n != 50 {
		t.Errorf("%d events", n)
	}
}
