package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cucumber/godog"
)

func (h *harness) createInitiativeWithBody(ctx context.Context, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/initiatives", body, actor)
}

func (h *harness) patchInitiative(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPatch, "/api/v1/initiatives/"+url.PathEscape(ref), body, actor)
}

// patchInitiativeByID patches the initiative whose slug is given through its
// ULID, upper or lower case.
func (h *harness) patchInitiativeByID(ctx context.Context, slug, lower, body, actor string) error {
	id, err := h.scalar(ctx, `SELECT id FROM initiatives WHERE slug = ?`, slug)
	if err != nil {
		return fmt.Errorf("read the id of %s: %w", slug, err)
	}
	if lower != "" {
		id = strings.ToLower(id)
	}
	return h.patchInitiative(ctx, id, body, actor)
}

const initiativeRowCounts = `SELECT (SELECT count(*) FROM initiatives) || '/' || (SELECT count(*) FROM initiative_tags) || '/' ||
	(SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM actors)`

func (h *harness) noteInitiativeCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, initiativeRowCounts)
	h.api.counts = got
	return err
}

func (h *harness) initiativeCountsUnchanged(ctx context.Context) error {
	got, err := h.scalar(ctx, initiativeRowCounts)
	if err != nil {
		return err
	}
	if h.api.counts == "" || got != h.api.counts {
		return fmt.Errorf("initiatives/tags/events/actors are %s, were %q", got, h.api.counts)
	}
	return nil
}

const initiativeEvents = `FROM events e JOIN initiatives i ON i.id = e.initiative_id WHERE e.type = ? AND i.slug = ?`

func (h *harness) initiativeEventCount(ctx context.Context, want int, typ, slug string) error {
	got, err := h.scalar(ctx, `SELECT count(*) `+initiativeEvents, typ, slug)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s %s events for %s, want %d", got, typ, slug, want)
	}
	return nil
}

func (h *harness) initiativeEventDataIs(ctx context.Context, typ, slug, key, want string) error {
	raw, err := h.scalar(ctx, `SELECT e.data `+initiativeEvents+` ORDER BY e.n DESC LIMIT 1`, typ, slug)
	if err != nil {
		return fmt.Errorf("read the %s event of %s: %w", typ, slug, err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return fmt.Errorf("event data %q: %w", raw, err)
	}
	if got := fieldText(data[key]); got != want {
		return fmt.Errorf("data.%s is %q, want %q", key, got, want)
	}
	return nil
}

func registerInitiativeEditSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I create the initiative with body "([^"]*)" as actor "([^"]*)"$`, h.createInitiativeWithBody)
	sc.Step(`^I patch the initiative "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.patchInitiative)
	sc.Step(`^I patch the initiative "([^"]*)" by its (lowercase )?id with body "([^"]*)" as actor "([^"]*)"$`, h.patchInitiativeByID)
	sc.Step(`^I note the counts of initiatives, tags, events and actors$`, h.noteInitiativeCounts)
	sc.Step(`^the counts of initiatives, tags, events and actors are unchanged$`, h.initiativeCountsUnchanged)
	sc.Step(`^exactly (\d+) ([a-z_]+) events? exists? for the initiative "([^"]*)"$`, h.initiativeEventCount)
	sc.Step(`^the latest ([a-z_]+) event of the initiative "([^"]*)" has data.([a-z_]+) "([^"]*)"$`, h.initiativeEventDataIs)
}
