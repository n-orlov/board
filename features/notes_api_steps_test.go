package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

func (h *harness) notePath(ref string) string {
	p := "/api/v1/notes"
	if ref != "" {
		p += "/" + url.PathEscape(ref)
	}
	return p
}

// rememberNote keeps the id of the note a 201 answered by its ref.
func (h *harness) rememberNote() error {
	if h.status != http.StatusCreated {
		return nil
	}
	var doc struct{ ID, Ref string }
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q: %w", h.api.body, err)
	}
	if h.api.ids == nil {
		h.api.ids = map[string]string{}
	}
	h.api.ids["note:"+doc.Ref] = doc.ID
	return nil
}

func (h *harness) postNote(ctx context.Context, body, actor string) error {
	if err := h.sendTask(ctx, http.MethodPost, h.notePath(""), body, actor); err != nil {
		return err
	}
	return h.rememberNote()
}

func (h *harness) postNoteWithoutBody(ctx context.Context, actor string) error {
	return h.postNote(ctx, "", actor)
}

func (h *harness) getNote(ctx context.Context, ref, actor string) error {
	return h.sendTask(ctx, http.MethodGet, h.notePath(ref), "", actor)
}

func (h *harness) getNoteByID(ctx context.Context, ref, how, actor string) error {
	id, ok := h.api.ids["note:"+ref]
	if !ok {
		return fmt.Errorf("no note %q was posted", ref)
	}
	if how == "lowercase " {
		id = strings.ToLower(id)
	}
	return h.getNote(ctx, id, actor)
}

var noteIDMarker = regexp.MustCompile(`\{id of (N\d+)\}`)

// listNotes lists notes; {id of N2} in the query stands for that note's ULID.
func (h *harness) listNotes(ctx context.Context, query, actor string) error {
	var missing error
	query = noteIDMarker.ReplaceAllStringFunc(query, func(m string) string {
		ref := noteIDMarker.FindStringSubmatch(m)[1]
		id, ok := h.api.ids["note:"+ref]
		if !ok {
			missing = fmt.Errorf("no note %q was posted", ref)
		}
		return id
	})
	if missing != nil {
		return missing
	}
	p := h.notePath("")
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

func (h *harness) nextNotesPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listNotes(ctx, query+"&after="+url.QueryEscape(after), actor)
}

func noteNumber(ref string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(ref, "N"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("note ref %q: %w", ref, err)
	}
	return n, nil
}

const noteCounts = `SELECT (SELECT count(*) FROM notes) || '/' || (SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM actors)`

func (h *harness) noteNoteCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, noteCounts)
	h.api.noteCounts = got
	return err
}

func (h *harness) noteCountsUnchanged(ctx context.Context) error {
	got, err := h.scalar(ctx, noteCounts)
	if err != nil {
		return err
	}
	if h.api.noteCounts == "" || got != h.api.noteCounts {
		return fmt.Errorf("notes/events/actors are %s, were %q", got, h.api.noteCounts)
	}
	return nil
}

func (h *harness) noteCount(ctx context.Context, want int) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM notes`)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s notes in the database, want %d", got, want)
	}
	return nil
}

func (h *harness) noteEventCount(ctx context.Context, want int, ref string) error {
	n, err := noteNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT count(*) FROM events e JOIN notes k ON k.id = e.ref_id WHERE e.type = 'note_posted' AND k.n = ?`, n)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s note_posted events for %s, want %d", got, ref, want)
	}
	return nil
}

// noteEventScope checks the task and initiative slug the note_posted event of
// the note carries, each "" for none.
func (h *harness) noteEventScope(ctx context.Context, ref, task, slug string) error {
	n, err := noteNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT COALESCE(i.slug || '-' || t.seq, '') || '|' || COALESCE(ei.slug, '') FROM events e
		JOIN notes k ON k.id = e.ref_id LEFT JOIN tasks t ON t.id = e.task_id LEFT JOIN initiatives i ON i.id = t.initiative_id
		LEFT JOIN initiatives ei ON ei.id = e.initiative_id WHERE e.type = 'note_posted' AND k.n = ?`, n)
	if err != nil {
		return fmt.Errorf("read the note_posted event of %s: %w", ref, err)
	}
	if want := task + "|" + slug; got != want {
		return fmt.Errorf("the note_posted event of %s is on %q, want %q", ref, got, want)
	}
	return nil
}

func (h *harness) noteEventPrincipal(ctx context.Context, ref, principal, actor string) error {
	n, err := noteNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT e.principal || '|' || a.handle FROM events e JOIN notes k ON k.id = e.ref_id
		JOIN actors a ON a.id = e.actor_id WHERE e.type = 'note_posted' AND k.n = ?`, n)
	if err != nil {
		return fmt.Errorf("read the note_posted event of %s: %w", ref, err)
	}
	if want := principal + "|" + actor; got != want {
		return fmt.Errorf("the note_posted event of %s has principal and actor %q, want %q", ref, got, want)
	}
	return nil
}

// noteEventSummary checks the one-line summary of the note's event.
func (h *harness) noteEventSummary(ctx context.Context, ref, want string) error {
	n, err := noteNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT e.summary FROM events e JOIN notes k ON k.id = e.ref_id WHERE e.type = 'note_posted' AND k.n = ?`, n)
	if err != nil {
		return fmt.Errorf("read the note_posted event of %s: %w", ref, err)
	}
	if got != want {
		return fmt.Errorf("the note_posted event of %s says %q, want %q", ref, got, want)
	}
	return nil
}

// notePostedAt moves a note's created_at, so a since time is exact.
func (h *harness) notePostedAt(ctx context.Context, ref, at string) error {
	n, err := noteNumber(ref)
	if err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	db, err := h.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	q := "UPDATE notes SET created_at = ? WHERE n = ?"
	if h.leg == "postgres" {
		q = "UPDATE notes SET created_at = $1 WHERE n = $2"
	}
	res, err := db.ExecContext(ctx, q, t.UnixMilli(), n)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c != 1 {
		return fmt.Errorf("%d rows updated for note %s, want 1", c, ref)
	}
	return nil
}

func registerNoteSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I post a note with body "([^"]*)" as actor "([^"]*)"$`, h.postNote)
	sc.Step(`^I post a note without a body as actor "([^"]*)"$`, h.postNoteWithoutBody)
	sc.Step(`^I request the note "([^"]*)" as actor "([^"]*)"$`, h.getNote)
	sc.Step(`^I request the note "([^"]*)" by its (lowercase )?id as actor "([^"]*)"$`, h.getNoteByID)
	sc.Step(`^I list the notes with "([^"]*)" as actor "([^"]*)"$`, h.listNotes)
	sc.Step(`^I request the next notes page with "([^"]*)" as actor "([^"]*)"$`, h.nextNotesPage)
	sc.Step(`^I note the counts of notes, events and actors$`, h.noteNoteCounts)
	sc.Step(`^the counts of notes, events and actors are unchanged$`, h.noteCountsUnchanged)
	sc.Step(`^there are (\d+) notes in the database$`, h.noteCount)
	sc.Step(`^exactly (\d+) note_posted events? exists? for the note "([^"]*)"$`, h.noteEventCount)
	sc.Step(`^the note_posted event of the note "([^"]*)" is on the task "([^"]*)" and the initiative "([^"]*)"$`, h.noteEventScope)
	sc.Step(`^the note_posted event of the note "([^"]*)" has principal "([^"]*)" and actor "([^"]*)"$`, h.noteEventPrincipal)
	sc.Step(`^the note_posted event of the note "([^"]*)" says "([^"]*)"$`, h.noteEventSummary)
	sc.Step(`^the note "([^"]*)" was posted at "([^"]*)"$`, h.notePostedAt)
}
