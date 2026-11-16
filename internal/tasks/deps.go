package tasks

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
)

// AddDep makes the task ref names blocked by the task blockedByRef names, in
// one transaction with a dep_added event on the dependent task whose
// data.blocked_by is the blocker's ref. The two tasks may be in different
// initiatives. Adding a dependency that exists succeeds and writes nothing.
// It returns ErrNotFound for an unknown task, ErrInvalid for a task depending
// on itself and a CycleError (errors.Is ErrConflict, naming the path) for a
// dependency that would close a cycle of any length, and writes nothing then.
// The returned task is the dependent, with Blocked derived.
func (s Store) AddDep(ctx context.Context, actor audit.Actor, principal, ref, blockedByRef string) (Task, error) {
	var out Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.addDep(ctx, tx, actor, principal, ref, blockedByRef)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

// RemoveDep removes the dependency of the task ref names on the task
// blockedByRef names, in one transaction with a dep_removed event on the
// dependent task whose data.blocked_by is the blocker's ref. It returns
// ErrNotFound for an unknown task or a dependency that does not exist, and
// writes nothing then.
func (s Store) RemoveDep(ctx context.Context, actor audit.Actor, principal, ref, blockedByRef string) (Task, error) {
	var out Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.removeDep(ctx, tx, actor, principal, ref, blockedByRef)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return out, nil
}

// pair reads the dependent and the blocker.
func (s Store) pair(ctx context.Context, tx *sql.Tx, ref, blockedByRef string) (task, blocker Task, err error) {
	if task, err = s.find(ctx, tx, ref); err != nil {
		return Task{}, Task{}, err
	}
	if blocker, err = s.find(ctx, tx, blockedByRef); err != nil {
		return Task{}, Task{}, err
	}
	return task, blocker, nil
}

func (s Store) hasDep(ctx context.Context, tx *sql.Tx, task, blocker Task) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(
		`SELECT COUNT(*) FROM task_deps WHERE task_id = ? AND blocked_by_task_id = ?`), task.ID, blocker.ID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("read dependency: %w", err)
	}
	return n > 0, nil
}

func (s Store) addDep(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, blockedByRef string) (Task, error) {
	task, blocker, err := s.pair(ctx, tx, ref, blockedByRef)
	if err != nil {
		return Task{}, err
	}
	if task.ID == blocker.ID {
		return Task{}, InvalidError{Msg: "task " + task.Ref + " cannot depend on itself", Next: "Send a different blocking task."}
	}
	if has, err := s.hasDep(ctx, tx, task, blocker); err != nil || has {
		return task, err
	}
	if err := s.refuseCycle(ctx, tx, task, blocker); err != nil {
		return Task{}, err
	}
	if err := s.writeDep(ctx, tx, actor, principal, task, blocker, EventDepAdded, "now blocked by",
		`INSERT INTO task_deps (task_id, blocked_by_task_id) VALUES (?, ?)`); err != nil {
		return Task{}, err
	}
	return s.find(ctx, tx, task.ID)
}

// writeDep runs the one-row change stmt of the dependency of task on blocker and
// appends its event, with the actor upserted.
func (s Store) writeDep(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, task, blocker Task, event, verb, stmt string) error {
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, d.Rebind(stmt), task.ID, blocker.ID); err != nil {
		return fmt.Errorf("write dependency: %w", err)
	}
	return s.appendDep(ctx, tx, actorID, principal, task, blocker, event, verb)
}

func (s Store) removeDep(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, blockedByRef string) (Task, error) {
	task, blocker, err := s.pair(ctx, tx, ref, blockedByRef)
	if err != nil {
		return Task{}, err
	}
	if has, err := s.hasDep(ctx, tx, task, blocker); err != nil {
		return Task{}, err
	} else if !has {
		return Task{}, NoDepError{Ref: task.Ref, BlockedBy: blocker.Ref}
	}
	if err := s.writeDep(ctx, tx, actor, principal, task, blocker, EventDepGone, "no longer blocked by",
		`DELETE FROM task_deps WHERE task_id = ? AND blocked_by_task_id = ?`); err != nil {
		return Task{}, err
	}
	return s.find(ctx, tx, task.ID)
}

func (s Store) appendDep(ctx context.Context, tx *sql.Tx, actorID, principal string, task, blocker Task, event, verb string) error {
	return audit.AppendAs(ctx, tx, s.Store.Dialect(), actorID, s.now(), audit.Event{
		Principal: principal, Type: event, InitiativeID: task.InitiativeID, TaskID: task.ID, RefID: task.ID,
		Summary: "task " + task.Ref + " " + verb + " " + blocker.Ref,
		Data:    map[string]any{"ref": task.Ref, "blocked_by": blocker.Ref},
	})
}

// edges reads every dependency as task id -> the ids it is blocked by.
func (s Store) edges(ctx context.Context, tx *sql.Tx) (map[string][]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT task_id, blocked_by_task_id FROM task_deps`)
	if err != nil {
		return nil, fmt.Errorf("read dependencies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	edges := map[string][]string{}
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, fmt.Errorf("read dependencies: %w", err)
		}
		edges[from] = append(edges[from], to)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read dependencies: %w", err)
	}
	return edges, nil
}

// refuseCycle returns a CycleError, with the path in refs, when making task
// blocked by blocker closes a cycle.
func (s Store) refuseCycle(ctx context.Context, tx *sql.Tx, task, blocker Task) error {
	edges, err := s.edges(ctx, tx)
	if err != nil {
		return err
	}
	path := domain.DependencyCycle(edges, task.ID, blocker.ID)
	if path == nil {
		return nil
	}
	refs := make([]string, len(path))
	for i, id := range path {
		t, err := s.find(ctx, tx, id)
		if err != nil {
			return err
		}
		refs[i] = t.Ref
	}
	return CycleError{Path: refs}
}
