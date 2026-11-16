package asks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/notify"
	"github.com/n-orlov/village-hall/internal/store"
)

// New is the request to raise an ask. Task is a task ref or ULID and Initiative
// a slug or ULID; both may be empty (a board-wide ask), and both together must
// agree. Mode "" is stop.
type New struct {
	Task       string
	Initiative string
	Question   string
	Details    string
	Options    []string
	Mode       string
	Blocking   bool
}

// Normalize checks every field and returns the request with the mode defaulted,
// the question trimmed and the options non-nil.
func (n New) Normalize() (New, error) {
	if n.Mode == "" {
		n.Mode = domain.AskModeStop
	}
	for _, c := range []struct {
		err  error
		next string
	}{
		{domain.ValidateQuestion(n.Question), "Send a question of 1 to 200 characters on one line, and put the rest in details."},
		{domain.ValidateDescription(n.Details), "Shorten the details to at most 20000 characters."},
		{domain.ValidateOptions(n.Options), "Send at most 10 options, each of 1 to 80 characters."},
		{domain.ValidateAskMode(n.Mode), "Send mode stop or poll, or leave it out for stop."},
	} {
		if c.err != nil {
			return New{}, InvalidError{Msg: c.err.Error(), Next: c.next}
		}
	}
	n.Question = strings.TrimSpace(n.Question)
	if n.Options == nil {
		n.Options = []string{}
	}
	return n, nil
}

// Raise inserts an open ask by actor and appends an ask_raised event, in one
// transaction. The initiative of an ask on a task is the task's. It returns
// ErrInvalid for a field that is wrong or a task and initiative that disagree,
// ErrNotFound for an unknown task or initiative and ErrConflict for a closed one
// (a closed task, or a closed initiative, with or without a task), and leaves no
// row, event or actor behind in any of those cases.
func (s Store) Raise(ctx context.Context, actor audit.Actor, principal string, in New) (Ask, error) {
	in, err := in.Normalize()
	if err != nil {
		return Ask{}, err
	}
	var out Ask
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		out, err = s.raise(ctx, tx, actor, principal, in)
		return err
	})
	return out, err
}

// scope is the task and initiative an ask is raised on.
type scope struct {
	taskID, taskRef, initiativeID, slug string
}

func (s Store) raise(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, in New) (Ask, error) {
	sc, err := s.scopeOf(ctx, tx, in)
	if err != nil {
		return Ask{}, err
	}
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return Ask{}, err
	}
	id, err := domain.NewULID(now)
	if err != nil {
		return Ask{}, err
	}
	opts, err := json.Marshal(in.Options)
	if err != nil {
		return Ask{}, fmt.Errorf("encode options: %w", err)
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("asks", []string{"id", "task_id", "initiative_id", "actor_id", "question",
		"details", "options", "mode", "blocking", "created_at"})),
		id, nullable(sc.taskID), nullable(sc.initiativeID), actorID, in.Question, in.Details, string(opts), in.Mode,
		d.EncodeBool(in.Blocking), store.EncodeTime(now))
	if err != nil {
		return Ask{}, fmt.Errorf("raise ask: %w", err)
	}
	k, err := s.find(ctx, tx, id)
	if err != nil {
		return Ask{}, err
	}
	return k, s.appendRaised(ctx, tx, actorID, principal, now, k, sc)
}

// appendRaised appends the ask_raised event of k and enqueues its asking notification.
func (s Store) appendRaised(ctx context.Context, tx *sql.Tx, actorID, principal string, now time.Time, k Ask, sc scope) error {
	d := s.Store.Dialect()
	eventID, err := audit.AppendAsID(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventRaised, InitiativeID: sc.initiativeID, TaskID: sc.taskID, RefID: k.ID,
		Summary: eventSummary(k, "raised", k.Question),
		Data:    raisedData(k),
	})
	if err != nil {
		return err
	}
	return notify.Enqueue(ctx, tx, s.Store, eventID, store.NotifyAsking, now)
}

// raisedData is the data of an ask_raised event.
func raisedData(k Ask) map[string]any {
	data := map[string]any{"ref": k.Ref, "question": k.Question, "mode": k.Mode, "blocking": k.Blocking}
	if k.Task != "" {
		data["task"] = k.Task
	}
	if k.Initiative != "" {
		data["initiative"] = k.Initiative
	}
	return data
}

// eventSummary is the one line of an ask event: "ask A3 raised on aa-1: text".
func eventSummary(k Ask, verb, text string) string {
	line := "ask " + k.Ref + " " + verb
	switch {
	case k.Task != "":
		line += " on " + k.Task
	case k.Initiative != "":
		line += " on " + k.Initiative
	}
	if text = domain.OneLine(text); text != "" {
		line += ": " + text
	}
	return line
}

// scopeOf resolves the task and initiative of the request through tx and
// refuses a closed task or initiative, or a task outside the initiative.
func (s Store) scopeOf(ctx context.Context, tx *sql.Tx, in New) (scope, error) {
	var sc scope
	if in.Task != "" {
		var err error
		if sc, err = s.taskScope(ctx, tx, in.Task); err != nil {
			return scope{}, err
		}
	}
	if in.Initiative != "" {
		var err error
		if sc, err = s.initiativeScope(ctx, tx, sc, in.Initiative); err != nil {
			return scope{}, err
		}
	}
	if sc.initiativeID == "" {
		return sc, nil
	}
	return sc, s.initiativeOpen(ctx, tx, sc)
}

// taskScope is the scope of the task ref names, refused when the task is closed.
func (s Store) taskScope(ctx context.Context, tx *sql.Tx, ref string) (scope, error) {
	t, err := s.findTask(ctx, tx, ref)
	if err != nil {
		return scope{}, err
	}
	if t.Status == "closed" {
		return scope{}, ConflictError{Msg: "cannot raise an ask on task " + t.Ref + ": it is closed",
			Next: "Reopen the task first, or raise the ask on an open task."}
	}
	return scope{taskID: t.ID, taskRef: t.Ref, initiativeID: t.InitiativeID, slug: t.Slug}, nil
}

// initiativeScope is sc with the initiative slug names, refused when a task
// already in sc belongs to another initiative.
func (s Store) initiativeScope(ctx context.Context, tx *sql.Tx, sc scope, slug string) (scope, error) {
	ini, err := (initiatives.Store{Store: s.Store}).FindIn(ctx, tx, slug)
	if errors.Is(err, initiatives.ErrNotFound) {
		return scope{}, fmt.Errorf("%w: initiative %s", ErrNotFound, slug)
	} else if err != nil {
		return scope{}, err
	}
	if sc.taskID != "" && sc.initiativeID != ini.ID {
		return scope{}, InvalidError{Msg: "task " + sc.taskRef + " does not belong to initiative " + ini.Slug,
			Next: "Send the task alone: its initiative is derived from it."}
	}
	sc.initiativeID, sc.slug = ini.ID, ini.Slug
	return sc, nil
}

// initiativeOpen returns a ConflictError unless the initiative of sc is open.
func (s Store) initiativeOpen(ctx context.Context, tx *sql.Tx, sc scope) error {
	var status string
	err := tx.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT status FROM initiatives WHERE id = ?`), sc.initiativeID).Scan(&status)
	switch {
	case err != nil:
		return fmt.Errorf("read initiative: %w", err)
	case status == "closed":
		return ConflictError{Msg: "cannot raise an ask on initiative " + sc.slug + ": it is closed",
			Next: "Reopen the initiative first, or raise the ask on an open one."}
	}
	return nil
}

// taskRow is the part of a task an ask needs.
type taskRow struct {
	ID, InitiativeID, Slug, Ref, Status string
}

// findTask reads the task a ref (`<slug>-<seq>` or ULID, either case) names.
func (s Store) findTask(ctx context.Context, q queryer, ref string) (taskRow, error) {
	parsed, err := domain.ParseTaskRef(ref)
	if err != nil {
		return taskRow{}, fmt.Errorf("%w: task %s", ErrNotFound, ref)
	}
	where, args := ` WHERE t.id = ?`, []any{parsed.ID}
	if parsed.ID == "" {
		where, args = ` WHERE i.slug = ? AND t.seq = ?`, []any{parsed.Slug, parsed.Seq}
	}
	var t taskRow
	var seq int
	err = q.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT t.id, t.initiative_id, i.slug, t.seq, t.status FROM tasks t
		JOIN initiatives i ON i.id = t.initiative_id`+where), args...).Scan(&t.ID, &t.InitiativeID, &t.Slug, &seq, &t.Status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return taskRow{}, fmt.Errorf("%w: task %s", ErrNotFound, ref)
	case err != nil:
		return taskRow{}, fmt.Errorf("read task: %w", err)
	}
	t.Ref = domain.FormatTaskRef(t.Slug, seq)
	return t, nil
}
