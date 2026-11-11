package features

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cucumber/godog"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/tasks"
)

// seedDoneWithLease migrates the scenario's database and writes a done task aa-1
// that holds a lease, which is live unless ended is set. The stores refuse to
// make that state (R14), so the task is marked done by an UPDATE, the way a
// Phase 2 release left it.
func (h *harness) seedDoneWithLease(ctx context.Context, holder string, ended bool) error {
	if err := h.freshDatabase(); err != nil {
		return err
	}
	return seedDoneTaskWithLease(ctx, h.dbURL, holder, ended)
}

// seedDoneTaskWithLease is seedDoneWithLease on the database at url.
func seedDoneTaskWithLease(ctx context.Context, url, holder string, ended bool) error {
	st, err := store.Open(url)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	actor := audit.Actor{Handle: holder}
	if _, err := (initiatives.Store{Store: st}).Create(ctx, actor, "key:seed", initiatives.New{Slug: "aa", Title: "First"}); err != nil {
		return err
	}
	task, err := (tasks.Store{Store: st}).Create(ctx, actor, "key:seed", tasks.New{Initiative: "aa", Title: "Held"})
	if err != nil {
		return err
	}
	lease, err := (leases.Store{Store: st}).Start(ctx, actor, "key:seed", leases.Start{Task: task.Ref, Intent: "work"})
	if err != nil {
		return err
	}
	if ended {
		if _, err := (leases.Store{Store: st}).End(ctx, actor, "key:seed", lease.Lease.Ref, "stopped"); err != nil {
			return err
		}
	}
	d := st.Dialect()
	_, err = st.Writer().ExecContext(ctx, d.Rebind(`UPDATE tasks SET status = 'done', done_at = updated_at, done_by = created_by WHERE id = ?`), task.ID)
	return err
}

// taskReopenedEvents reads the task_reopened events of the task ref names, as
// the handle of each event's actor, its principal and its data.reason.
func (h *harness) taskReopenedEvents(ctx context.Context, ref string) ([][3]string, error) {
	st, err := store.Open(h.dbURL)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	d := st.Dialect()
	rows, err := st.Reader().QueryContext(ctx, d.Rebind(`SELECT a.handle, e.principal, e.data FROM events e JOIN actors a ON a.id = e.actor_id
		JOIN tasks t ON t.id = e.task_id JOIN initiatives i ON i.id = t.initiative_id
		WHERE e.type = 'task_reopened' AND i.slug || '-' || CAST(t.seq AS TEXT) = ? ORDER BY e.id`), ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][3]string
	for rows.Next() {
		var handle, principal, data string
		if err := rows.Scan(&handle, &principal, &data); err != nil {
			return nil, err
		}
		out = append(out, [3]string{handle, principal, data})
	}
	return out, rows.Err()
}

func (h *harness) taskStatusIs(ctx context.Context, ref, want string) error {
	st, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	got, err := (tasks.Store{Store: st}).Get(ctx, ref)
	if err != nil {
		return err
	}
	if got.Status != want {
		return fmt.Errorf("task %s is %s, want %s", ref, got.Status, want)
	}
	return nil
}

func (h *harness) reopenedEventsAre(ctx context.Context, ref string, n int, handle, principal, reason string) error {
	got, err := h.taskReopenedEvents(ctx, ref)
	if err != nil {
		return err
	}
	if len(got) != n {
		return fmt.Errorf("task %s has %d task_reopened events, want %d: %v", ref, len(got), n, got)
	}
	for _, e := range got {
		if e[0] != handle || e[1] != principal || !jsonHas(e[2], "reason", reason) {
			return fmt.Errorf("task_reopened event = %v, want actor %s, principal %s, data.reason %q", e, handle, principal, reason)
		}
	}
	return nil
}

func (h *harness) reopenedCount(ctx context.Context, ref string, n int) error {
	got, err := h.taskReopenedEvents(ctx, ref)
	if err != nil {
		return err
	}
	if len(got) != n {
		return fmt.Errorf("task %s has %d task_reopened events, want %d", ref, len(got), n)
	}
	return nil
}

func (h *harness) serverRestarted(ctx context.Context) (context.Context, error) {
	endServer(h.server)
	return h.startServer(ctx)
}

func registerServeUpgradeSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^a Phase 2 database holding the done task "([^"]*)" with a live lease held by "([^"]*)"$`,
		func(ctx context.Context, _, holder string) error { return h.seedDoneWithLease(ctx, holder, false) })
	sc.Step(`^a Phase 2 database holding the done task "([^"]*)" whose lease ended$`,
		func(ctx context.Context, _ string) error { return h.seedDoneWithLease(ctx, "claude@laptop", true) })
	sc.Step(`^the board server is stopped and started again$`, h.serverRestarted)
	sc.Step(`^the task "([^"]*)" is (open|done)$`, h.taskStatusIs)
	sc.Step(`^the task "([^"]*)" has exactly (\d+) task_reopened events?, made by "([^"]*)" under principal "([^"]*)" with the reason "([^"]*)"$`, h.reopenedEventsAre)
	sc.Step(`^the task "([^"]*)" has exactly (\d+) task_reopened events?$`, h.reopenedCount)
}

// jsonHas reports whether the JSON object text has key set to the string want.
func jsonHas(text, key, want string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return false
	}
	return m[key] == want
}
