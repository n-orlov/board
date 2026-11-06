package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/store"
)

// DroppedResolution is the resolution a forced initiative close gives the tasks
// that were not closed.
const DroppedResolution = "dropped"

// CloseInitiative closes the open initiative ref names, appending an
// initiative_closed event. An initiative with a task that is not closed is
// closed only with force, which first closes each such task as dropped (each
// with its task_closed event, in task order); without force it is an
// initiatives.ErrConflict naming how many are open and done. All of it runs in
// one transaction, so a refusal changes nothing. It returns
// initiatives.ErrNotFound for an unknown initiative and initiatives.ErrConflict
// for a closed one.
func (s Store) CloseInitiative(ctx context.Context, actor audit.Actor, principal, ref string, force bool) (initiatives.Initiative, error) {
	var id string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.closeInitiative(ctx, tx, actor, principal, ref, force)
		return err
	})
	if err != nil {
		return initiatives.Initiative{}, err
	}
	return initiatives.Store{Store: s.Store, Now: s.Now}.Get(ctx, id)
}

func (s Store) closeInitiative(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, force bool) (string, error) {
	inits := initiatives.Store{Store: s.Store, Now: s.Now}
	cur, err := inits.FindIn(ctx, tx, ref)
	if err != nil {
		return "", err
	}
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return "", err
	}
	if err := inits.MarkClosed(ctx, tx, cur, actorID, store.EncodeTime(now)); err != nil {
		return "", err
	}
	dropped, err := s.dropLeft(ctx, tx, actor, principal, cur, force)
	if err != nil {
		return "", err
	}
	onBehalf, err := audit.OnBehalf(ctx, tx, d, actor, now)
	if err != nil {
		return "", err
	}
	return cur.ID, audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, OnBehalfOfID: onBehalf, Type: initiatives.EventClosed, InitiativeID: cur.ID, RefID: cur.ID,
		Summary: "initiative " + cur.Slug + " closed",
		Data:    map[string]any{"slug": cur.Slug, "force": force, "dropped": dropped},
	})
}

// dropLeft closes as dropped each task of the initiative that is not closed and
// returns how many it closed. Without force a task that is not closed is the
// refusal instead.
func (s Store) dropLeft(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, ini initiatives.Initiative, force bool) (int, error) {
	left, err := s.notClosed(ctx, tx, ini.ID)
	if err != nil {
		return 0, err
	}
	if len(left) > 0 && !force {
		return 0, stillOpen(ini.Slug, left)
	}
	for _, id := range left {
		if _, err := s.transitInTx(ctx, tx, actor, principal, id, closeTransition(DroppedResolution)); err != nil {
			return 0, err
		}
	}
	return len(left), nil
}

// notClosed returns the ids of the initiative's tasks that are open or done, in
// task order.
func (s Store) notClosed(ctx context.Context, tx *sql.Tx, initiativeID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT id FROM tasks WHERE initiative_id = ? AND status <> 'closed' ORDER BY seq`), initiativeID)
	if err != nil {
		return nil, fmt.Errorf("read tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read tasks: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// stillOpen is the refusal to close an initiative that has tasks left.
func stillOpen(slug string, left []string) error {
	return initiatives.ConflictError{
		Msg:  "cannot close initiative " + slug + ": " + strconv.Itoa(len(left)) + " of its tasks are not closed",
		Next: "Close those tasks first, or send {\"force\": true} to close them as dropped along with the initiative.",
	}
}
