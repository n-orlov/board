package perf

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
)

// SeedPlan is what Seed writes: Events events (Notes of them the note_posted
// events of Notes notes), Leases live leases and the actors, initiatives and
// tasks they hang on. Now is the instant the data ends at; the events run back
// from it over SeedSpan.
type SeedPlan struct {
	Events, Notes, Leases int
	Now                   time.Time
}

// SeedSpan is how far back the seeded history starts.
const SeedSpan = 30 * 24 * time.Hour

// Seeded actors and shapes the harness depends on.
const (
	// ActorHandle is the actor the harness acts as; it holds the first leases.
	ActorHandle = "perf-agent-00"
	// actorCount is how many agent actors the seed makes.
	actorCount = 20
	// initiativeCount is how many initiatives the tasks are spread over.
	initiativeCount = 5
	// ownResources is how many leases carry a resource no other lease holds: the
	// harness actor's, so its check-in has no conflict (and `vh now` exits 0).
	ownResources = 10
	// sharedResources is how many resources the other leases share between them.
	sharedResources = 40
	// doneTasks, openTasks and watchTasks are the tasks without a lease.
	doneTasks, openTasks, watchTasks = 100, 100, 20
	// batch is how many events are written per transaction.
	batch = 50_000
)

// Validate refuses a plan the seeder cannot write.
func (p SeedPlan) Validate() error {
	switch {
	case p.Events < 1 || p.Notes < 0 || p.Leases < 1:
		return fmt.Errorf("seed plan needs at least 1 event and 1 lease, got %d events, %d leases", p.Events, p.Leases)
	case p.Notes > p.Events:
		return fmt.Errorf("seed plan has %d notes for %d events", p.Notes, p.Events)
	case p.Now.IsZero():
		return fmt.Errorf("seed plan has no end time")
	}
	return nil
}

// seeder carries the ids Seed made so far.
type seeder struct {
	db     *sql.DB
	plan   SeedPlan
	start  time.Time
	actors []string
	inis   []string
	tasks  []string // leased tasks first, then the rest
	leases []string
}

// Seed writes the plan into a migrated SQLite store through its writer
// connection. It speeds the load up by relaxing synchronous mode for that
// connection, which only matters for a crash mid-seed, and leaves the rest of
// the store as the product made it.
func Seed(ctx context.Context, db *sql.DB, plan SeedPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	s := &seeder{db: db, plan: plan, start: plan.Now.Add(-SeedSpan)}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous=OFF"); err != nil {
		return err
	}
	steps := []func(context.Context, *sql.Tx) error{s.actorsAndInitiatives, s.tasksAndLeases}
	for _, step := range steps {
		if err := s.inTx(ctx, step); err != nil {
			return err
		}
	}
	return s.history(ctx)
}

func (s *seeder) inTx(ctx context.Context, f func(context.Context, *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := f(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *seeder) ulid(offset time.Duration) string {
	id, err := domain.NewULID(s.start.Add(offset))
	if err != nil {
		panic(err) // the OS CSPRNG failed: nothing sensible can follow
	}
	return id
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func (s *seeder) actorsAndInitiatives(ctx context.Context, tx *sql.Tx) error {
	for i := 0; i < actorCount; i++ {
		id := s.ulid(time.Duration(i) * time.Second)
		s.actors = append(s.actors, id)
		if _, err := tx.ExecContext(ctx, `INSERT INTO actors (id, handle, kind, first_seen_at, last_seen_at) VALUES (?, ?, 'agent', ?, ?)`,
			id, fmt.Sprintf("perf-agent-%02d", i), ms(s.start), ms(s.plan.Now)); err != nil {
			return err
		}
	}
	for i := 0; i < initiativeCount; i++ {
		id := s.ulid(time.Minute + time.Duration(i)*time.Second)
		s.inis = append(s.inis, id)
		if _, err := tx.ExecContext(ctx, `INSERT INTO initiatives (id, slug, title, created_by, created_at, next_task_seq) VALUES (?, ?, ?, ?, ?, ?)`,
			id, fmt.Sprintf("perf-%d", i), fmt.Sprintf("Perf initiative %d", i), s.actors[0], ms(s.start), s.taskSeqEnd(i)); err != nil {
			return err
		}
	}
	return nil
}

// taskTotal is how many tasks the plan makes.
func (s *seeder) taskTotal() int { return s.plan.Leases + doneTasks + openTasks + watchTasks }

// taskSeqEnd is the next_task_seq of initiative i once its tasks exist: task t
// belongs to initiative t % initiativeCount and has seq t / initiativeCount + 1.
func (s *seeder) taskSeqEnd(i int) int {
	n := 0
	for t := i; t < s.taskTotal(); t += initiativeCount {
		n++
	}
	return n + 1
}

// taskKind says which group task t is in: it has a lease, is done and not
// closed, is open, or is a watch that is overdue.
func (s *seeder) taskGroup(t int) string {
	switch {
	case t < s.plan.Leases:
		return "leased"
	case t < s.plan.Leases+doneTasks:
		return "done"
	case t < s.plan.Leases+doneTasks+openTasks:
		return "open"
	}
	return "watch"
}

const insertTask = `INSERT INTO tasks (id, initiative_id, seq, title, kind, status, resolution, cadence_seconds, next_check_at,
	created_by, created_at, updated_at, done_at, done_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (s *seeder) tasksAndLeases(ctx context.Context, tx *sql.Tx) error {
	for t := 0; t < s.taskTotal(); t++ {
		id := s.ulid(2*time.Minute + time.Duration(t)*time.Millisecond)
		s.tasks = append(s.tasks, id)
		args := s.taskArgs(t, id)
		if _, err := tx.ExecContext(ctx, insertTask, args...); err != nil {
			return err
		}
	}
	for l := 0; l < s.plan.Leases; l++ {
		if err := s.lease(ctx, tx, l); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) taskArgs(t int, id string) []any {
	created := ms(s.start.Add(2 * time.Minute))
	var kind, status, resolution, doneBy any = "work", "open", nil, nil
	var cadence, next, doneAt any
	switch s.taskGroup(t) {
	case "done":
		status, resolution, doneAt, doneBy = "done", "completed", created+1000, s.actors[0]
	case "watch":
		kind, cadence, next = "watch", 3600, ms(s.plan.Now.Add(-24*time.Hour))
	}
	return []any{id, s.inis[t%initiativeCount], t/initiativeCount + 1, fmt.Sprintf("Perf task %d", t), kind, status, resolution,
		cadence, next, s.actors[0], created, created, doneAt, doneBy}
}

// leaseResource is the resource lease l holds: the harness actor's leases hold
// one of their own, the others share sharedResources between them.
func leaseResource(l int) string {
	if l < ownResources {
		return fmt.Sprintf("res-%d", l)
	}
	return fmt.Sprintf("shared-%d", l%sharedResources)
}

func (s *seeder) lease(ctx context.Context, tx *sql.Tx, l int) error {
	id := s.ulid(time.Hour + time.Duration(l)*time.Millisecond)
	s.leases = append(s.leases, id)
	const ttl = 43200 // the longest lease, so none expires during a run
	_, err := tx.ExecContext(ctx, `INSERT INTO leases (id, task_id, actor_id, intent, ttl_seconds, started_at, renewed_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, s.tasks[l], s.actors[l/ownResources%actorCount], fmt.Sprintf("Working on perf task %d", l), ttl,
		ms(s.plan.Now), ms(s.plan.Now), ms(s.plan.Now.Add(ttl*time.Second)))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO lease_resources (lease_id, resource) VALUES (?, ?)`, id, leaseResource(l))
	return err
}

// history writes the events and the notes among them, batch events per
// transaction. Event e happens at start + e * step; every stride-th event is the
// note_posted event of the next note, written with it.
func (s *seeder) history(ctx context.Context) error {
	for from := 0; from < s.plan.Events; from += batch {
		to := min(from+batch, s.plan.Events)
		if err := s.inTx(ctx, func(ctx context.Context, tx *sql.Tx) error { return s.events(ctx, tx, from, to) }); err != nil {
			return err
		}
	}
	return nil
}

const insertEvent = `INSERT INTO events (id, at, actor_id, principal, initiative_id, task_id, type, ref_id, summary, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const insertNote = `INSERT INTO notes (id, actor_id, summary, body, task_id, initiative_id, resource, mentions, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (s *seeder) events(ctx context.Context, tx *sql.Tx, from, to int) error {
	ev, err := tx.PrepareContext(ctx, insertEvent)
	if err != nil {
		return err
	}
	defer func() { _ = ev.Close() }()
	nt, err := tx.PrepareContext(ctx, insertNote)
	if err != nil {
		return err
	}
	defer func() { _ = nt.Close() }()
	step := SeedSpan / time.Duration(s.plan.Events)
	for e := from; e < to; e++ {
		at := 3*time.Hour + time.Duration(e)*step*9/10
		id, row := s.ulid(at), s.eventRow(e)
		if note, ok := s.noteOf(e); ok {
			noteID := s.ulid(at)
			if _, err := nt.ExecContext(ctx, noteArgs(noteID, note, s, at)...); err != nil {
				return err
			}
			row = s.noteEvent(note, noteID)
		}
		if _, err := ev.ExecContext(ctx, append([]any{id, ms(s.start.Add(at))}, row...)...); err != nil {
			return err
		}
	}
	return nil
}

// eventRow is the columns after id and at of filler event e: a lease renewal on
// a leased task, or a task creation.
func (s *seeder) eventRow(e int) []any {
	t := e % s.plan.Leases
	typ, summary := "lease_renewed", fmt.Sprintf("renewed the lease on perf task %d", t)
	if e%3 == 0 {
		typ, summary = "task_created", fmt.Sprintf("created perf task %d", t)
	}
	actor := s.actors[t/ownResources%actorCount]
	return []any{actor, "key:perf", s.inis[t%initiativeCount], s.tasks[t], typ, s.leases[t], summary, `{}`}
}

// noteKind is where note n is posted: a broadcast, on a task, on a resource of
// an initiative, a broadcast that mentions the harness actor, or on an
// initiative.
type noteKind struct {
	n    int
	kind int
}

// noteOf returns the note that event e is the posting of, when it is one.
func (s *seeder) noteOf(e int) (noteKind, bool) {
	if s.plan.Notes == 0 {
		return noteKind{}, false
	}
	stride := s.plan.Events / s.plan.Notes
	if e%stride != 0 || e/stride >= s.plan.Notes {
		return noteKind{}, false
	}
	n := e / stride
	return noteKind{n: n, kind: n % 10}, true
}

type noteShape struct {
	task, initiative, resource, mentions, broadcast any
}

// shape is the binding of a note: kind 0 is a broadcast, 1 to 4 sit on a leased
// task, 5 and 6 on a resource of an initiative, 7 mentions the harness actor and
// 8 and 9 sit on an initiative.
func (s *seeder) shape(k noteKind) noteShape {
	sh := noteShape{mentions: "[]", broadcast: true}
	switch {
	case k.kind >= 1 && k.kind <= 4:
		sh.task, sh.broadcast = s.tasks[k.n%s.plan.Leases], false
	case k.kind == 5 || k.kind == 6:
		sh.initiative, sh.resource, sh.broadcast = s.inis[k.n%initiativeCount], leaseResource(k.n%s.plan.Leases), false
	case k.kind == 7:
		sh.mentions = `["` + ActorHandle + `"]`
	case k.kind >= 8:
		sh.initiative, sh.broadcast = s.inis[k.n%initiativeCount], false
	}
	return sh
}

func noteArgs(id string, k noteKind, s *seeder, at time.Duration) []any {
	sh := s.shape(k)
	var body any
	if k.n%5 == 0 {
		body = fmt.Sprintf("Perf note %d body. %s", k.n, filler)
	}
	return []any{id, s.actors[k.n%actorCount], noteSummary(k.n), body, sh.task, sh.initiative, sh.resource, sh.mentions, ms(s.start.Add(at))}
}

const filler = "Notes are posted by agents as they work, and this text only gives the row the weight of a real one so the table and its indexes are the size a busy board has."

func noteSummary(n int) string { return fmt.Sprintf("Perf note %d: progress on the work in hand", n) }

// noteEvent is the columns after id and at of the note_posted event of a note.
func (s *seeder) noteEvent(k noteKind, noteID string) []any {
	sh := s.shape(k)
	data, _ := json.Marshal(map[string]any{"summary": noteSummary(k.n), "broadcast": sh.broadcast})
	ini := sh.initiative
	if sh.task != nil {
		ini = s.inis[k.n%s.plan.Leases%initiativeCount]
	}
	return []any{s.actors[k.n%actorCount], "key:perf", ini, sh.task, "note_posted", noteID, noteSummary(k.n), string(data)}
}
