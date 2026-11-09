package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/cucumber/godog"
)

var cursorIDMarker = regexp.MustCompile(`\{id of ([NE])(\d+)\}`)

// expandCursorIDs replaces {id of N2} and {id of E7} with the ULID of that note
// or event, read from the database.
func (h *harness) expandCursorIDs(ctx context.Context, s string) (string, error) {
	var failed error
	out := cursorIDMarker.ReplaceAllStringFunc(s, func(m string) string {
		parts := cursorIDMarker.FindStringSubmatch(m)
		n, _ := strconv.Atoi(parts[2])
		id, err := h.cursorTarget(ctx, parts[1], n)
		if err != nil {
			failed = err
		}
		return id
	})
	return out, failed
}

func (h *harness) cursorTarget(ctx context.Context, kind string, n int) (string, error) {
	table := map[string]string{"N": "notes", "E": "events"}[kind]
	id, err := h.scalar(ctx, `SELECT id FROM `+table+` WHERE n = ?`, n)
	if err != nil {
		return "", fmt.Errorf("read the id of %s%d: %w", kind, n, err)
	}
	return id, nil
}

// putCursor sends PUT /api/v1/cursors/{stream} with a raw JSON body (single
// quotes stand for double quotes, {id of N2} for that note's ULID).
func (h *harness) putCursor(ctx context.Context, stream, body, actor string) error {
	body, err := h.expandCursorIDs(ctx, body)
	if err != nil {
		return err
	}
	return h.sendTask(ctx, http.MethodPut, "/api/v1/cursors/"+url.PathEscape(stream), body, actor)
}

func (h *harness) putCursorWithoutBody(ctx context.Context, stream, actor string) error {
	return h.putCursor(ctx, stream, "", actor)
}

// cursorHolds checks the position stored for the actor on the stream: the ULID
// of the note or event ref names.
func (h *harness) cursorHolds(ctx context.Context, stream, actor, ref string) error {
	kind, n := ref[:1], 0
	if _, err := fmt.Sscanf(ref[1:], "%d", &n); err != nil {
		return fmt.Errorf("ref %q: %w", ref, err)
	}
	want, err := h.cursorTarget(ctx, kind, n)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT c.position FROM cursors c JOIN actors a ON a.id = c.actor_id WHERE a.handle = ? AND c.stream = ?`, actor, stream)
	if err != nil {
		return fmt.Errorf("read the %s cursor of %s: %w", stream, actor, err)
	}
	if got != want {
		return fmt.Errorf("the %s cursor of %s holds %s, want %s (%s)", stream, actor, got, want, ref)
	}
	return nil
}

func (h *harness) cursorUnset(ctx context.Context, stream, actor string) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM cursors c JOIN actors a ON a.id = c.actor_id WHERE a.handle = ? AND c.stream = ?`, actor, stream)
	if err != nil {
		return err
	}
	if got != "0" {
		return fmt.Errorf("the %s cursor of %s is set, want none", stream, actor)
	}
	return nil
}

func (h *harness) cursorRows(ctx context.Context, want int) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM cursors`)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s cursors in the database, want %d", got, want)
	}
	return nil
}

func registerCursorSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I put the "([^"]*)" cursor with body "([^"]*)" as actor "([^"]*)"$`, h.putCursor)
	sc.Step(`^I put the "([^"]*)" cursor without a body as actor "([^"]*)"$`, h.putCursorWithoutBody)
	sc.Step(`^the "([^"]*)" cursor of "([^"]*)" holds the id of "([EN]\d+)"$`, h.cursorHolds)
	sc.Step(`^the "([^"]*)" cursor of "([^"]*)" is not set$`, h.cursorUnset)
	sc.Step(`^there are (\d+) cursors in the database$`, h.cursorRows)
}
