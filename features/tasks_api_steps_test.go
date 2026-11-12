package features

import (
	"bytes"
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

// repeated is the <<text*N>> and <<text*N;separator>> forms a scenario uses to
// write a long value, such as a 201-character title, without typing it.
var repeated = regexp.MustCompile(`<<(.*?)\*(\d+)(?:;(.*?))?>>`)

// expandBody turns the repeat forms into the text they stand for and the single
// quotes a feature uses for JSON strings into double quotes.
func expandBody(s string) string {
	s = repeated.ReplaceAllStringFunc(s, func(m string) string {
		parts := repeated.FindStringSubmatch(m)
		n, _ := strconv.Atoi(parts[2])
		return strings.Join(repeatText(parts[1], n), parts[3])
	})
	return jsonQuotes(s)
}

func repeatText(text string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = text
	}
	return out
}

// sendTask sends a request with a raw JSON body (or none) and brackets it in time.
func (h *harness) sendTask(ctx context.Context, method, path, body, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	var payload any
	if body != "" {
		payload = json.RawMessage(expandBody(body))
	}
	h.api.sentAt = time.Now()
	err := h.sendKeyed(ctx, method, path, h.key, actor, payload)
	h.api.answeredAt = time.Now()
	return err
}

// rememberTask keeps the id of the task a 201 answered by its ref.
func (h *harness) rememberTask() error {
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
	h.api.ids["task:"+doc.Ref] = doc.ID
	return nil
}

func (h *harness) createTaskWithBody(ctx context.Context, body, actor string) error {
	if err := h.sendTask(ctx, http.MethodPost, "/api/v1/tasks", body, actor); err != nil {
		return err
	}
	return h.rememberTask()
}

func (h *harness) createTask(ctx context.Context, initiative, title, actor string) error {
	body, _ := json.Marshal(map[string]any{"initiative": initiative, "title": title})
	return h.createTaskWithBody(ctx, string(body), actor)
}

func (h *harness) createTaskByInitiativeID(ctx context.Context, slug, how, title, actor string) error {
	id, ok := h.api.ids[slug]
	if !ok {
		return fmt.Errorf("no initiative %q was created", slug)
	}
	if how == "lowercase " {
		id = strings.ToLower(id)
	}
	return h.createTask(ctx, id, title, actor)
}

func (h *harness) createWatchTask(ctx context.Context, initiative, title string, cadence int, actor string) error {
	body, _ := json.Marshal(map[string]any{"initiative": initiative, "title": title, "kind": "watch", "cadence_seconds": cadence})
	return h.createTaskWithBody(ctx, string(body), actor)
}

func (h *harness) getTask(ctx context.Context, ref, actor string) error {
	return h.sendTask(ctx, http.MethodGet, "/api/v1/tasks/"+url.PathEscape(ref), "", actor)
}

func (h *harness) getTaskByID(ctx context.Context, ref, how, actor string) error {
	id, ok := h.api.ids["task:"+ref]
	if !ok {
		return fmt.Errorf("no task %q was created", ref)
	}
	if how == "lowercase " {
		id = strings.ToLower(id)
	}
	return h.getTask(ctx, id, actor)
}

func (h *harness) patchTask(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPatch, "/api/v1/tasks/"+url.PathEscape(ref), body, actor)
}

// responsePath walks a dotted path through the last response: a name for an
// object, a number for an array.
func (h *harness) responsePath(path string) (any, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(h.api.body))
	dec.UseNumber()
	var cur any
	if err := dec.Decode(&cur); err != nil {
		return nil, false, fmt.Errorf("response %q: %w", h.api.body, err)
	}
	for _, step := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[step]
			if !ok {
				return nil, false, nil
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false, nil
			}
			cur = v[i]
		default:
			return nil, false, nil
		}
	}
	return cur, true, nil
}

func (h *harness) pathIs(path, want string) error {
	got, ok, err := h.responsePath(path)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the response %s has no %q", h.api.body, path)
	}
	if text := fieldText(got); text != want {
		return fmt.Errorf("%q is %q, want %q (response %s)", path, text, want, h.api.body)
	}
	return nil
}

func (h *harness) pathIsAbsent(path string) error {
	got, ok, err := h.responsePath(path)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%q is %v, want it absent (response %s)", path, got, h.api.body)
	}
	return nil
}

func (h *harness) pathHasItems(path string, n int) error {
	got, ok, err := h.responsePath(path)
	if err != nil {
		return err
	}
	list, isList := got.([]any)
	if !ok || !isList || len(list) != n {
		return fmt.Errorf("%q is %v, want a list of %d (response %s)", path, got, n, h.api.body)
	}
	return nil
}

// pathRefs checks that the objects of a list have these refs, in order.
func (h *harness) pathRefs(path, want string) error {
	got, _, err := h.responsePath(path)
	if err != nil {
		return err
	}
	list, ok := got.([]any)
	if !ok {
		return fmt.Errorf("%q is %v, want a list (response %s)", path, got, h.api.body)
	}
	refs := make([]string, len(list))
	for i, e := range list {
		obj, _ := e.(map[string]any)
		refs[i] = fmt.Sprint(obj["ref"])
	}
	if text := strings.Join(refs, ","); text != strings.Join(splitList(want), ",") {
		return fmt.Errorf("%q lists refs %q, want %q", path, text, want)
	}
	return nil
}

// nextCheckIsNowPlus checks next_check_at is the request time plus the cadence,
// to the millisecond the database keeps.
func (h *harness) nextCheckIsNowPlus(seconds int) error {
	got, ok, err := h.responsePath("next_check_at")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no next_check_at in %s", h.api.body)
	}
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(got))
	if err != nil {
		return err
	}
	d := time.Duration(seconds) * time.Second
	if at.Before(h.api.sentAt.Add(d).Truncate(time.Millisecond)) || at.After(h.api.answeredAt.Add(d)) {
		return fmt.Errorf("next_check_at %s is not between the request time %s and %s plus %s", at, h.api.sentAt, h.api.answeredAt, d)
	}
	return nil
}

const rowCounts = `SELECT (SELECT count(*) FROM tasks) || '/' || (SELECT count(*) FROM task_deps) || '/' ||
	(SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM actors)`

func (h *harness) noteCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, rowCounts)
	h.api.counts = got
	return err
}

func (h *harness) countsUnchanged(ctx context.Context) error {
	got, err := h.scalar(ctx, rowCounts)
	if err != nil {
		return err
	}
	if h.api.counts == "" || got != h.api.counts {
		return fmt.Errorf("tasks/deps/events/actors are %s, were %q", got, h.api.counts)
	}
	return nil
}

func (h *harness) eventCount(ctx context.Context, want int, typ, ref string) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM events e JOIN tasks t ON t.id = e.task_id JOIN initiatives i ON i.id = t.initiative_id
		WHERE e.type = ? AND i.slug || '-' || t.seq = ?`, typ, ref)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s %s events for %s, want %d", got, typ, ref, want)
	}
	return nil
}

// latestEventData is the data object of the newest event of a type on a task.
func (h *harness) latestEventData(ctx context.Context, typ, ref string) (map[string]any, error) {
	raw, err := h.scalar(ctx, `SELECT e.data FROM events e JOIN tasks t ON t.id = e.task_id JOIN initiatives i ON i.id = t.initiative_id
		WHERE e.type = ? AND i.slug || '-' || t.seq = ? ORDER BY e.n DESC LIMIT 1`, typ, ref)
	if err != nil {
		return nil, fmt.Errorf("read the %s event of %s: %w", typ, ref, err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("event data %q: %w", raw, err)
	}
	return data, nil
}

func (h *harness) eventDataIs(ctx context.Context, typ, ref, key, want string) error {
	data, err := h.latestEventData(ctx, typ, ref)
	if err != nil {
		return err
	}
	if got := fieldText(data[key]); got != want {
		return fmt.Errorf("data.%s is %q, want %q", key, got, want)
	}
	return nil
}

func registerTaskAPISteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I create the task with body "([^"]*)" as actor "([^"]*)"$`, h.createTaskWithBody)
	sc.Step(`^I create a task in "([^"]*)" titled "([^"]*)" as actor "([^"]*)"$`, h.createTask)
	sc.Step(`^I create a task in the initiative "([^"]*)" by its (lowercase )?id titled "([^"]*)" as actor "([^"]*)"$`, h.createTaskByInitiativeID)
	sc.Step(`^I create a watch task in "([^"]*)" titled "([^"]*)" with a cadence of (\d+) seconds as actor "([^"]*)"$`, h.createWatchTask)
	sc.Step(`^I request the task "([^"]*)" as actor "([^"]*)"$`, h.getTask)
	sc.Step(`^I request the task "([^"]*)" by its (lowercase )?id as actor "([^"]*)"$`, func(ctx context.Context, ref, how, actor string) error {
		return h.getTaskByID(ctx, ref, how, actor)
	})
	sc.Step(`^I patch the task "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.patchTask)
	sc.Step(`^the response path "([^"]*)" is "([^"]*)"$`, h.pathIs)
	sc.Step(`^the response path "([^"]*)" is absent$`, h.pathIsAbsent)
	sc.Step(`^the response path "([^"]*)" has (\d+) items$`, h.pathHasItems)
	sc.Step(`^the response path "([^"]*)" lists the refs "([^"]*)"$`, h.pathRefs)
	sc.Step(`^the response next_check_at is the request time plus (\d+) seconds$`, h.nextCheckIsNowPlus)
	sc.Step(`^I note the counts of tasks, dependencies, events and actors$`, h.noteCounts)
	sc.Step(`^the counts of tasks, dependencies, events and actors are unchanged$`, h.countsUnchanged)
	sc.Step(`^exactly (\d+) ([a-z_]+) events? exists? for the task "([^"]*)"$`, h.eventCount)
	sc.Step(`^the latest ([a-z_]+) event of "([^"]*)" has data.([a-z_]+) "([^"]*)"$`, h.eventDataIs)
	registerTaskTransitionSteps(sc, h)
	registerTaskDepSteps(sc, h)
	registerTaskEventSteps(sc, h)
	registerLeaseSteps(sc, h)
	registerAskSteps(sc, h)
	registerAskWaitSteps(sc, h)
	registerNoteSteps(sc, h)
	registerCursorSteps(sc, h)
	registerNowSteps(sc, h)
	registerAttentionSteps(sc, h)
	registerTimelineSteps(sc, h)
	registerTaskListSteps(sc, h)
	registerInitiativeListSteps(sc, h)
}
