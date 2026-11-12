package features

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cucumber/godog"
)

// requireKey is the remembered key, or an error when a scenario has none.
func (h *harness) requireKey() error {
	if h.key == "" {
		return errors.New("no key was remembered")
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// createInitiative posts a new initiative and remembers its id by slug.
func (h *harness) createInitiative(ctx context.Context, slug, title, tags, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	payload := map[string]any{"slug": slug, "title": title}
	if list := splitList(tags); len(list) > 0 {
		payload["tags"] = list
	}
	if err := h.sendKeyed(ctx, http.MethodPost, "/api/v1/initiatives", h.key, actor, payload); err != nil {
		return err
	}
	if h.status != http.StatusCreated {
		return nil
	}
	var doc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q: %w", h.api.body, err)
	}
	if h.api.ids == nil {
		h.api.ids = map[string]string{}
	}
	h.api.ids[slug] = doc.ID
	return nil
}

func (h *harness) getInitiative(ctx context.Context, ref, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	return h.sendKeyed(ctx, http.MethodGet, "/api/v1/initiatives/"+url.PathEscape(ref), h.key, actor, nil)
}

func (h *harness) getInitiativeByID(ctx context.Context, slug, actor string) error {
	id, ok := h.api.ids[slug]
	if !ok {
		return fmt.Errorf("no initiative %q was created", slug)
	}
	return h.getInitiative(ctx, id, actor)
}

func (h *harness) getInitiativeByLowercaseID(ctx context.Context, slug, actor string) error {
	id, ok := h.api.ids[slug]
	if !ok {
		return fmt.Errorf("no initiative %q was created", slug)
	}
	return h.getInitiative(ctx, strings.ToLower(id), actor)
}

func (h *harness) listInitiatives(ctx context.Context, query, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	path := "/api/v1/initiatives"
	if query != "" {
		path += "?" + query
	}
	return h.sendKeyed(ctx, http.MethodGet, path, h.key, actor, nil)
}

// listNextPage requests the page the last response's next_after names.
func (h *harness) listNextPage(ctx context.Context, query, actor string) error {
	var doc struct {
		NextAfter string `json:"next_after"`
	}
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("the last response %q: %w", h.api.body, err)
	}
	if doc.NextAfter == "" {
		return fmt.Errorf("the last response %q has no next_after", h.api.body)
	}
	return h.listInitiatives(ctx, query+"&after="+url.QueryEscape(doc.NextAfter), actor)
}

func (h *harness) bodyMap() (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return nil, fmt.Errorf("response %q is not a JSON object: %w", h.api.body, err)
	}
	return doc, nil
}

// fieldText renders a top-level field: a string as it is, a list joined by commas.
func fieldText(v any) string {
	list, ok := v.([]any)
	if !ok {
		return fmt.Sprint(v)
	}
	parts := make([]string, len(list))
	for i, e := range list {
		parts[i] = fmt.Sprint(e)
	}
	return strings.Join(parts, ",")
}

func (h *harness) responseFieldIs(name, want string) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	got, ok := doc[name]
	if !ok {
		return fmt.Errorf("the response %s has no field %q", h.api.body, name)
	}
	if text := fieldText(got); text != want {
		return fmt.Errorf("field %q is %q, want %q", name, text, want)
	}
	return nil
}

func (h *harness) responseFieldHasLength(name string, n int) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	if got, _ := doc[name].(string); len(got) != n {
		return fmt.Errorf("field %q is %q, want %d characters", name, got, n)
	}
	return nil
}

func (h *harness) responseListsInitiatives(want string) error {
	var doc struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q: %w", h.api.body, err)
	}
	slugs := make([]string, len(doc.Items))
	for i, it := range doc.Items {
		slugs[i] = it.Slug
	}
	if got := strings.Join(slugs, ","); got != strings.Join(splitList(want), ",") {
		return fmt.Errorf("the response lists %q, want %q", got, want)
	}
	return nil
}

func (h *harness) responseHasNoNextAfter() error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	if v, has := doc["next_after"]; has {
		return fmt.Errorf("the response has next_after %v, want none", v)
	}
	return nil
}

func (h *harness) responseNextAfterIsIDOf(slug string) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	if got, want := doc["next_after"], h.api.ids[slug]; want == "" || got != want {
		return fmt.Errorf("next_after is %v, want the id of %q (%q)", got, slug, want)
	}
	return nil
}

func (h *harness) problemNextContains(want string) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	if got, _ := doc["next"].(string); !strings.Contains(got, want) {
		return fmt.Errorf("problem next %q does not contain %q (body %s)", got, want, h.api.body)
	}
	return nil
}

func (h *harness) problemDetailContains(want string) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	if got, _ := doc["detail"].(string); !strings.Contains(got, want) {
		return fmt.Errorf("problem detail %q does not contain %q", got, want)
	}
	return nil
}

// scalar runs a query with at most one argument that returns one value.
func (h *harness) scalar(ctx context.Context, query string, args ...any) (string, error) {
	db, err := h.openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	if h.leg == "postgres" {
		for i := range args {
			query = strings.Replace(query, "?", fmt.Sprintf("$%d", i+1), 1)
		}
	}
	var got string
	err = db.QueryRowContext(ctx, query, args...).Scan(&got)
	return got, err
}

func (h *harness) createdEventHas(ctx context.Context, slug, principal, actor string) error {
	got, err := h.scalar(ctx, `SELECT e.principal || '|' || a.handle FROM events e JOIN actors a ON a.id = e.actor_id
		JOIN initiatives i ON i.id = e.initiative_id WHERE e.type = 'initiative_created' AND i.slug = ?`, slug)
	if err != nil {
		return fmt.Errorf("read the initiative_created event of %q: %w", slug, err)
	}
	if want := principal + "|" + actor; got != want {
		return fmt.Errorf("the event has principal|actor %q, want %q", got, want)
	}
	return nil
}

// nextRequestCarries makes the next API request send one more header.
func (h *harness) nextRequestCarries(header, value string) {
	if h.api.headers == nil {
		h.api.headers = map[string]string{}
	}
	h.api.headers[header] = jsonQuotes(value)
}

// jsonQuotes turns the single quotes a feature uses for JSON strings into double quotes.
func jsonQuotes(s string) string { return strings.ReplaceAll(s, "'", `"`) }

func (h *harness) actorStores(ctx context.Context, handle, wayBack, meta string) error {
	got, err := h.scalar(ctx, `SELECT COALESCE(way_back, '') || '|' || meta FROM actors WHERE handle = ?`, handle)
	if err != nil {
		return fmt.Errorf("read the actor %q: %w", handle, err)
	}
	if want := wayBack + "|" + jsonQuotes(meta); got != want {
		return fmt.Errorf("actor %q holds way_back|meta %q, want %q", handle, got, want)
	}
	return nil
}

func (h *harness) actorHasOneRow(ctx context.Context, handle string) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM actors WHERE handle = ?`, handle)
	if err != nil {
		return err
	}
	if got != "1" {
		return fmt.Errorf("%s rows for actor %q, want 1", got, handle)
	}
	return nil
}

// createdEventCount counts the initiative_created events besides the inbox's: serve writes that one
// on start (SPEC 4.2), before any scenario step.
func (h *harness) createdEventCount(ctx context.Context, want int) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM events WHERE type = 'initiative_created'
		AND initiative_id NOT IN (SELECT id FROM initiatives WHERE slug = 'inbox')`)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s initiative_created events besides the inbox's, want %d", got, want)
	}
	return nil
}

func registerInitiativeAPISteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I create the initiative "([^"]*)" titled "([^"]*)" with tags "([^"]*)" as actor "([^"]*)"$`, h.createInitiative)
	sc.Step(`^I create the initiative "([^"]*)" titled "([^"]*)" with no tags as actor "([^"]*)"$`,
		func(ctx context.Context, slug, title, actor string) error {
			return h.createInitiative(ctx, slug, title, "", actor)
		})
	sc.Step(`^I request the initiative "([^"]*)" as actor "([^"]*)"$`, h.getInitiative)
	sc.Step(`^I request the initiative "([^"]*)" by its id as actor "([^"]*)"$`, h.getInitiativeByID)
	sc.Step(`^I request the initiative "([^"]*)" by its lowercase id as actor "([^"]*)"$`, h.getInitiativeByLowercaseID)
	sc.Step(`^I request the initiatives list "([^"]*)" as actor "([^"]*)"$`, h.listInitiatives)
	sc.Step(`^I request the next page with "([^"]*)" as actor "([^"]*)"$`, h.listNextPage)
	sc.Step(`^the response field "([^"]*)" is "([^"]*)"$`, h.responseFieldIs)
	sc.Step(`^the response has an "([^"]*)" of (\d+) characters$`, h.responseFieldHasLength)
	sc.Step(`^the response lists the initiatives "([^"]*)"$`, h.responseListsInitiatives)
	sc.Step(`^the response has no next_after$`, h.responseHasNoNextAfter)
	sc.Step(`^the response next_after is the id of "([^"]*)"$`, h.responseNextAfterIsIDOf)
	sc.Step(`^the problem has a next field containing "([^"]*)"$`, h.problemNextContains)
	sc.Step(`^the problem detail contains "([^"]*)"$`, h.problemDetailContains)
	sc.Step(`^the initiative_created event for "([^"]*)" has principal "([^"]*)" and actor "([^"]*)"$`, h.createdEventHas)
	sc.Step(`^the actor "([^"]*)" has exactly one row in actors$`, h.actorHasOneRow)
	sc.Step(`^the next API request carries the header "([^"]*)" with value "([^"]*)"$`, h.nextRequestCarries)
	sc.Step(`^the actor "([^"]*)" stores the way back "([^"]*)" and the meta "([^"]*)"$`, h.actorStores)
	sc.Step(`^exactly (\d+) initiative_created event exists besides the inbox's$`, h.createdEventCount)
	sc.Step(`^no initiative_created event exists besides the inbox's$`, func(ctx context.Context) error { return h.createdEventCount(ctx, 0) })
}
