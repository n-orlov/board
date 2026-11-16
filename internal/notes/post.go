package notes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/store"
)

// New is the request to post a note. Task is a task ref or ULID and Initiative
// a slug or ULID; both may be empty (a broadcast), and both together must
// agree. Resource is an optional `<type>:<name>` string and Mentions a list of
// actor handles. A resource that is supplied, even as an empty or blank string
// (ResourceSet, or any non-empty Resource), must be 1 to 100 characters once
// trimmed; leaving it out (an empty Resource without ResourceSet) is allowed.
type New struct {
	Task       string
	Initiative string
	Summary    string
	Body       string
	Resource   string
	// ResourceSet marks a resource that was supplied even if it is empty.
	ResourceSet bool
	Mentions    []string
}

// Normalize checks every field and returns the request with the summary,
// resource and mentions trimmed and the mentions non-nil.
func (n New) Normalize() (New, error) {
	supplied := n.ResourceSet || n.Resource != ""
	n.Resource = strings.TrimSpace(n.Resource)
	for _, c := range []struct {
		err  error
		next string
	}{
		{domain.ValidateNoteSummary(n.Summary), "Send a summary of 1 to 200 characters on one line, and put the rest in body."},
		{domain.ValidateNoteBody(n.Body), "Shorten the body to at most 20000 characters."},
		{resourceError(n.Resource, supplied), "Send a resource of 1 to 100 characters such as host:build-1, or leave it out."},
	} {
		if c.err != nil {
			return New{}, InvalidError{Msg: c.err.Error(), Next: c.next}
		}
	}
	m, err := domain.NormalizeMentions(n.Mentions)
	if err != nil {
		return New{}, invalid(err, "Send at most 20 actor handles without spaces.")
	}
	n.Summary, n.Mentions = strings.TrimSpace(n.Summary), m
	return n, nil
}

func resourceError(r string, supplied bool) error {
	if c := utf8.RuneCountInString(r); supplied && (c < 1 || c > domain.MaxResourceLen) {
		return fmt.Errorf("resource must be 1 to %d characters, got %d", domain.MaxResourceLen, c)
	}
	return nil
}

// Post inserts a note by actor and appends a note_posted event, in one
// transaction, whether the note is bound to a task, to an initiative, to a
// resource or to none of them (a broadcast). The initiative of a note on a task
// is the task's. It returns ErrInvalid for a field that is wrong or a task and
// initiative that disagree, ErrNotFound for an unknown task or initiative, and
// leaves no row, event or actor behind in any of those cases.
func (s Store) Post(ctx context.Context, actor audit.Actor, principal string, in New) (Note, error) {
	in, err := in.Normalize()
	if err != nil {
		return Note{}, err
	}
	var out Note
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		out, err = s.post(ctx, tx, actor, principal, in)
		return err
	})
	return out, err
}

func (s Store) post(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, in New) (Note, error) {
	taskID, iniID, err := s.scopeOf(ctx, tx, in)
	if err != nil {
		return Note{}, err
	}
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return Note{}, err
	}
	id, err := domain.NewULID(now)
	if err != nil {
		return Note{}, err
	}
	mentions, err := json.Marshal(in.Mentions)
	if err != nil {
		return Note{}, fmt.Errorf("encode mentions: %w", err)
	}
	_, err = tx.ExecContext(ctx, d.Rebind(d.Insert("notes", []string{"id", "actor_id", "summary", "body", "task_id",
		"initiative_id", "resource", "mentions", "created_at"})),
		id, actorID, in.Summary, nullable(in.Body), nullable(taskID), nullable(iniID), nullable(in.Resource), string(mentions),
		store.EncodeTime(now))
	if err != nil {
		return Note{}, fmt.Errorf("post note: %w", err)
	}
	k, err := s.find(ctx, tx, id)
	if err != nil {
		return Note{}, err
	}
	err = audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventPosted, InitiativeID: iniID, TaskID: taskID, RefID: id,
		Summary: eventSummary(k), Data: postedData(k),
	})
	return k, err
}

// postedData is the data of a note_posted event: the note's ref, summary and
// whatever it is bound to, never its body.
func postedData(k Note) map[string]any {
	data := map[string]any{"ref": k.Ref, "summary": k.Summary, "broadcast": k.Broadcast}
	for key, v := range map[string]string{"task": k.Task, "initiative": k.Initiative, "resource": k.Resource} {
		if v != "" {
			data[key] = v
		}
	}
	if len(k.Mentions) > 0 {
		data["mentions"] = k.Mentions
	}
	return data
}

// eventSummary is the one line of a note_posted event: "note N3 on aa-1: text".
func eventSummary(k Note) string {
	line := "note " + k.Ref
	switch {
	case k.Task != "":
		line += " on " + k.Task
	case k.Initiative != "":
		line += " on " + k.Initiative
	case k.Resource != "":
		line += " on " + k.Resource
	default:
		line += " (broadcast)"
	}
	return line + ": " + domain.OneLine(k.Summary)
}

// scopeOf resolves the task and initiative ids of the request through tx and
// refuses a task outside the initiative.
func (s Store) scopeOf(ctx context.Context, tx *sql.Tx, in New) (taskID, iniID string, err error) {
	var t taskRow
	if in.Task != "" {
		if t, err = s.findTask(ctx, tx, in.Task); err != nil {
			return "", "", err
		}
		taskID, iniID = t.ID, t.InitiativeID
	}
	if in.Initiative == "" {
		return taskID, iniID, nil
	}
	ini, err := s.findInitiative(ctx, tx, in.Initiative)
	if err != nil {
		return "", "", err
	}
	if taskID != "" && t.InitiativeID != ini.ID {
		return "", "", InvalidError{Msg: "task " + t.Ref + " does not belong to initiative " + ini.Slug,
			Next: "Send the task alone: its initiative is derived from it."}
	}
	return taskID, ini.ID, nil
}

// findInitiative reads the initiative slug names through tx.
func (s Store) findInitiative(ctx context.Context, tx *sql.Tx, slug string) (initiatives.Initiative, error) {
	ini, err := (initiatives.Store{Store: s.Store}).FindIn(ctx, tx, slug)
	if errors.Is(err, initiatives.ErrNotFound) {
		return ini, fmt.Errorf("%w: initiative %s", ErrNotFound, slug)
	}
	return ini, err
}

// taskRow is the part of a task a note needs.
type taskRow struct {
	ID, InitiativeID, Ref string
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
	var slug string
	var seq int
	err = q.QueryRowContext(ctx, s.Store.Dialect().Rebind(`SELECT t.id, t.initiative_id, i.slug, t.seq FROM tasks t
		JOIN initiatives i ON i.id = t.initiative_id`+where), args...).Scan(&t.ID, &t.InitiativeID, &slug, &seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return taskRow{}, fmt.Errorf("%w: task %s", ErrNotFound, ref)
	case err != nil:
		return taskRow{}, fmt.Errorf("read task: %w", err)
	}
	t.Ref = domain.FormatTaskRef(slug, seq)
	return t, nil
}
