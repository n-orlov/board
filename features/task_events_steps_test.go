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

func (h *harness) taskSubPath(ref, sub string) string {
	return "/api/v1/tasks/" + url.PathEscape(ref) + "/" + sub
}

func (h *harness) recordCheck(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.taskSubPath(ref, "checks"), body, actor)
}

func (h *harness) recordMilestone(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.taskSubPath(ref, "milestones"), body, actor)
}

func (h *harness) recordCheckWithoutBody(ctx context.Context, ref, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.taskSubPath(ref, "checks"), "", actor)
}

// eventOrdinal is the n of the ordinal-th event written, 1 being the first.
func (h *harness) eventOrdinal(ctx context.Context, ordinal int) (string, error) {
	if ordinal < 1 {
		return "", fmt.Errorf("event ordinal %d is not positive", ordinal)
	}
	return h.scalar(ctx, fmt.Sprintf(`SELECT n FROM events ORDER BY n LIMIT 1 OFFSET %d`, ordinal-1))
}

var eventPlaceholder = regexp.MustCompile(`\{(id|ref):(\d+)\}`)

// expandEventQuery turns {id:N} and {ref:N} into the ULID and the E<n> ref of
// the Nth event written, so a scenario names an event by its position.
func (h *harness) expandEventQuery(ctx context.Context, q string) (string, error) {
	var failure error
	out := eventPlaceholder.ReplaceAllStringFunc(q, func(m string) string {
		parts := eventPlaceholder.FindStringSubmatch(m)
		ordinal, _ := strconv.Atoi(parts[2])
		n, err := h.eventOrdinal(ctx, ordinal)
		if err != nil {
			failure = fmt.Errorf("%s: %w", m, err)
			return m
		}
		if parts[1] == "ref" {
			return "E" + n
		}
		id, err := h.scalar(ctx, `SELECT id FROM events WHERE n = `+n)
		if err != nil {
			failure = fmt.Errorf("%s: %w", m, err)
		}
		return id
	})
	return out, failure
}

func (h *harness) getEvents(ctx context.Context, base, query, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	query, err := h.expandEventQuery(ctx, query)
	if err != nil {
		return err
	}
	if query != "" {
		base += "?" + query
	}
	return h.sendKeyed(ctx, http.MethodGet, base, h.key, actor, nil)
}

func (h *harness) listEvents(ctx context.Context, query, actor string) error {
	return h.getEvents(ctx, "/api/v1/events", query, actor)
}

func (h *harness) listTaskEvents(ctx context.Context, ref, query, actor string) error {
	return h.getEvents(ctx, h.taskSubPath(ref, "events"), query, actor)
}

func (h *harness) listTaskEventsPlain(ctx context.Context, ref, actor string) error {
	return h.listTaskEvents(ctx, ref, "", actor)
}

// nextAfter is the next_after of the last response.
func (h *harness) nextAfter() (string, error) {
	var doc struct {
		NextAfter string `json:"next_after"`
	}
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return "", fmt.Errorf("the last response %q: %w", h.api.body, err)
	}
	if doc.NextAfter == "" {
		return "", fmt.Errorf("the last response %q has no next_after", h.api.body)
	}
	return doc.NextAfter, nil
}

func (h *harness) nextEventsPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listEvents(ctx, query+"&after="+url.QueryEscape(after), actor)
}

func (h *harness) nextTaskEventsPage(ctx context.Context, ref, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listTaskEvents(ctx, ref, query+"&after="+url.QueryEscape(after), actor)
}

// backdateEvents sets the time of the first count events written.
func (h *harness) backdateEvents(ctx context.Context, count int, when string) error {
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return err
	}
	db, err := h.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	q := `UPDATE events SET at = ? WHERE n IN (SELECT n FROM events ORDER BY n LIMIT ?)`
	if h.leg == "postgres" {
		q = `UPDATE events SET at = $1 WHERE n IN (SELECT n FROM events ORDER BY n LIMIT $2)`
	}
	res, err := db.ExecContext(ctx, q, at.UnixMilli(), count)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); int(n) != count {
		return fmt.Errorf("%d events dated, want %d", n, count)
	}
	return nil
}

// itemField lists one field of every item of the last response, comma-joined.
func (h *harness) itemField(field string) (string, error) {
	doc, err := h.bodyMap()
	if err != nil {
		return "", err
	}
	list, ok := doc["items"].([]any)
	if !ok {
		return "", fmt.Errorf("the response %s has no items list", h.api.body)
	}
	out := make([]string, len(list))
	for i, it := range list {
		obj, _ := it.(map[string]any)
		out[i] = fmt.Sprint(obj[field])
	}
	return strings.Join(out, ","), nil
}

func (h *harness) itemsHave(field, want string) error {
	got, err := h.itemField(field)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the items have %s %q, want %q", field, got, want)
	}
	return nil
}

// itemsAreEvents checks the items are the events at these positions, in order.
func (h *harness) itemsAreEvents(ctx context.Context, positions string) error {
	var want []string
	for _, p := range splitList(positions) {
		ordinal, err := strconv.Atoi(p)
		if err != nil {
			return err
		}
		n, err := h.eventOrdinal(ctx, ordinal)
		if err != nil {
			return err
		}
		want = append(want, "E"+n)
	}
	return h.itemsHave("ref", strings.Join(want, ","))
}

func (h *harness) nextAfterIsEvent(ctx context.Context, ordinal int) error {
	n, err := h.eventOrdinal(ctx, ordinal)
	if err != nil {
		return err
	}
	want, err := h.scalar(ctx, `SELECT id FROM events WHERE n = `+n)
	if err != nil {
		return err
	}
	got, err := h.nextAfter()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("next_after is %q, want the id of event %d (%q)", got, ordinal, want)
	}
	return nil
}

// itemShape checks the first item carries the documented fields with the
// documented types, and the optional ones only when they have a value.
func (h *harness) itemShape(ordinal int, optional string) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	list, _ := doc["items"].([]any)
	if len(list) < ordinal {
		return fmt.Errorf("the response %s has no item %d", h.api.body, ordinal)
	}
	item, _ := list[ordinal-1].(map[string]any)
	for _, f := range []string{"id", "ref", "at", "type", "actor", "principal", "summary"} {
		if s, ok := item[f].(string); !ok || s == "" {
			return fmt.Errorf("item %d has no text %q: %v", ordinal, f, item)
		}
	}
	if _, ok := item["data"].(map[string]any); !ok {
		return fmt.Errorf("item %d has no data object: %v", ordinal, item)
	}
	if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(item["at"])); err != nil {
		return fmt.Errorf("item %d at %v: %w", ordinal, item["at"], err)
	}
	if !regexp.MustCompile(`^E\d+$`).MatchString(fmt.Sprint(item["ref"])) {
		return fmt.Errorf("item %d ref %v is not E<n>", ordinal, item["ref"])
	}
	for _, f := range splitList(optional) {
		if s, ok := item[f].(string); !ok || s == "" {
			return fmt.Errorf("item %d has no %q: %v", ordinal, f, item)
		}
	}
	return nil
}

func (h *harness) itemLacks(ordinal int, field string) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	list, _ := doc["items"].([]any)
	if len(list) < ordinal {
		return fmt.Errorf("the response %s has no item %d", h.api.body, ordinal)
	}
	item, _ := list[ordinal-1].(map[string]any)
	if v, has := item[field]; has {
		return fmt.Errorf("item %d has %s %v, want it absent", ordinal, field, v)
	}
	return nil
}

func registerTaskEventSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I record a check on the task "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.recordCheck)
	sc.Step(`^I record a check on the task "([^"]*)" without a body as actor "([^"]*)"$`, h.recordCheckWithoutBody)
	sc.Step(`^I record a milestone on the task "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.recordMilestone)
	sc.Step(`^I request the events of the task "([^"]*)" as actor "([^"]*)"$`, h.listTaskEventsPlain)
	sc.Step(`^I request the events of the task "([^"]*)" with "([^"]*)" as actor "([^"]*)"$`, h.listTaskEvents)
	sc.Step(`^I request the events list "([^"]*)" as actor "([^"]*)"$`, h.listEvents)
	sc.Step(`^I request the next events page with "([^"]*)" as actor "([^"]*)"$`, h.nextEventsPage)
	sc.Step(`^I request the next events page of the task "([^"]*)" with "([^"]*)" as actor "([^"]*)"$`, h.nextTaskEventsPage)
	sc.Step(`^the first (\d+) events are dated "([^"]*)"$`, h.backdateEvents)
	sc.Step(`^the response items have the types "([^"]*)"$`, func(want string) error { return h.itemsHave("type", want) })
	sc.Step(`^the response items have the actors "([^"]*)"$`, func(want string) error { return h.itemsHave("actor", want) })
	sc.Step(`^the response items are the events "([^"]*)"$`, h.itemsAreEvents)
	sc.Step(`^the response next_after is the id of the event (\d+)$`, h.nextAfterIsEvent)
	sc.Step(`^item (\d+) of the response is a complete event with "([^"]*)"$`, h.itemShape)
	sc.Step(`^item (\d+) of the response has no "([^"]*)"$`, h.itemLacks)
}
