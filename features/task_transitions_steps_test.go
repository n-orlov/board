package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/cucumber/godog"
)

func (h *harness) transition(ctx context.Context, ref, verb, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(ref)+"/"+verb, body, actor)
}

func (h *harness) markDone(ctx context.Context, ref, actor string) error {
	return h.transition(ctx, ref, "done", "", actor)
}

func (h *harness) markDoneWithBody(ctx context.Context, ref, body, actor string) error {
	return h.transition(ctx, ref, "done", body, actor)
}

func (h *harness) reopen(ctx context.Context, ref, actor string) error {
	return h.transition(ctx, ref, "reopen", "", actor)
}

func (h *harness) closeWithBody(ctx context.Context, ref, body, actor string) error {
	return h.transition(ctx, ref, "close", body, actor)
}

func (h *harness) closeWithoutBody(ctx context.Context, ref, actor string) error {
	return h.transition(ctx, ref, "close", "", actor)
}

// onBehalfIs checks the stored on_behalf_of of the newest event of a type on a
// task: the handle of the actor it names, or "" for none.
func (h *harness) onBehalfIs(ctx context.Context, typ, ref, want string) error {
	got, err := h.scalar(ctx, `SELECT COALESCE(ob.handle, '') FROM events e JOIN tasks t ON t.id = e.task_id
		JOIN initiatives i ON i.id = t.initiative_id LEFT JOIN actors ob ON ob.id = e.on_behalf_of
		WHERE e.type = ? AND i.slug || '-' || t.seq = ? ORDER BY e.n DESC LIMIT 1`, typ, ref)
	if err != nil {
		return fmt.Errorf("read the %s event of %s: %w", typ, ref, err)
	}
	if got != want {
		return fmt.Errorf("the %s event of %s is on behalf of %q, want %q", typ, ref, got, want)
	}
	return nil
}

func (h *harness) pathIsPresent(path string) error {
	got, ok, err := h.responsePath(path)
	if err != nil {
		return err
	}
	if !ok || fmt.Sprint(got) == "" {
		return fmt.Errorf("%q is missing or empty in %s", path, h.api.body)
	}
	return nil
}

func (h *harness) actorHasNoRow(ctx context.Context, handle string) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM actors WHERE handle = ?`, handle)
	if err != nil {
		return err
	}
	if got != "0" {
		return fmt.Errorf("%s rows for actor %q, want none", got, handle)
	}
	return nil
}

func registerTaskTransitionSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I mark the task "([^"]*)" done as actor "([^"]*)"$`, h.markDone)
	sc.Step(`^I mark the task "([^"]*)" done with body "([^"]*)" as actor "([^"]*)"$`, h.markDoneWithBody)
	sc.Step(`^I reopen the task "([^"]*)" as actor "([^"]*)"$`, h.reopen)
	sc.Step(`^I close the task "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.closeWithBody)
	sc.Step(`^I close the task "([^"]*)" without a body as actor "([^"]*)"$`, h.closeWithoutBody)
	sc.Step(`^the response path "([^"]*)" is present$`, h.pathIsPresent)
	sc.Step(`^the actor "([^"]*)" has no row in actors$`, h.actorHasNoRow)
	sc.Step(`^the latest ([a-z_]+) event of "([^"]*)" is on behalf of "([^"]*)"$`, h.onBehalfIs)
	sc.Step(`^the latest ([a-z_]+) event of "([^"]*)" is on behalf of nobody$`, func(ctx context.Context, typ, ref string) error {
		return h.onBehalfIs(ctx, typ, ref, "")
	})
}
