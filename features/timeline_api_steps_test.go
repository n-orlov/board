package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// daysFromNow is the instant n days from now, before it for a negative n.
func daysFromNow(n int) time.Time { return time.Now().Add(time.Duration(n) * 24 * time.Hour) }

// getTimeline reads GET /api/v1/timeline with the query, which may be empty.
func (h *harness) getTimeline(ctx context.Context, query, actor string) error {
	p := "/api/v1/timeline"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// getTimelineWindow reads the timeline from the instant a number of days from
// now to another, each negative for the past, with more query after them.
func (h *harness) getTimelineWindow(ctx context.Context, from, to int, more, actor string) error {
	q := url.Values{}
	q.Set("from", daysFromNow(from).UTC().Format(time.RFC3339))
	q.Set("to", daysFromNow(to).UTC().Format(time.RFC3339))
	query := q.Encode()
	if more != "" {
		query += "&" + more
	}
	return h.getTimeline(ctx, query, actor)
}

// taskIDQuery selects the id of the task a ref names, for a statement that takes
// the slug and the number after it.
const taskIDQuery = `(SELECT t.id FROM tasks t JOIN initiatives i ON i.id = t.initiative_id WHERE i.slug = ? AND t.seq = ?)`

func (h *harness) updateOne(ctx context.Context, what, query string, args ...any) error {
	rows, err := h.exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%d rows changed for %s, want 1", rows, what)
	}
	return nil
}

func slugAndSeq(ref string) (string, string, error) {
	dash := strings.LastIndex(ref, "-")
	if dash < 0 {
		return "", "", fmt.Errorf("%q is not a task ref", ref)
	}
	return ref[:dash], ref[dash+1:], nil
}

// createdDaysAgo backdates the created_at of a task, into the future for a negative n.
func (h *harness) createdDaysAgo(ctx context.Context, ref string, days int) error {
	slug, seq, err := slugAndSeq(ref)
	if err != nil {
		return err
	}
	return h.updateOne(ctx, ref, `UPDATE tasks SET created_at = ? WHERE seq = ? AND initiative_id = (SELECT id FROM initiatives WHERE slug = ?)`,
		daysFromNow(-days).UnixMilli(), seq, slug)
}

// plannedBetween sets the planned span of a task, in days from now.
func (h *harness) plannedBetween(ctx context.Context, ref string, from, to int) error {
	slug, seq, err := slugAndSeq(ref)
	if err != nil {
		return err
	}
	return h.updateOne(ctx, ref, `UPDATE tasks SET planned_start = ?, planned_end = ? WHERE seq = ? AND initiative_id = (SELECT id FROM initiatives WHERE slug = ?)`,
		daysFromNow(from).UnixMilli(), daysFromNow(to).UnixMilli(), seq, slug)
}

// leaseRan sets the span of a lease: it started a number of days ago and ended
// (an ended lease) or expired (any other) the later number of days ago.
func (h *harness) leaseRan(ctx context.Context, ref string, started, ended int) error {
	n, err := leaseNumber(ref)
	if err != nil {
		return err
	}
	end := daysFromNow(-ended).UnixMilli()
	return h.updateOne(ctx, ref, `UPDATE leases SET started_at = ?, renewed_at = ?, expires_at = ?, ended_at = CASE WHEN state = 'ended' THEN ? ELSE ended_at END WHERE n = ?`,
		daysFromNow(-started).UnixMilli(), daysFromNow(-started).UnixMilli(), end, end, n)
}

// eventsHappened moves the events of a type on a task whose summary contains
// the text to a number of days ago.
func (h *harness) eventsHappened(ctx context.Context, typ, ref, contains string, days int) error {
	slug, seq, err := slugAndSeq(ref)
	if err != nil {
		return err
	}
	rows, err := h.exec(ctx, `UPDATE events SET at = ? WHERE type = ? AND summary LIKE ? AND task_id = `+taskIDQuery,
		daysFromNow(-days).UnixMilli(), typ, "%"+contains+"%", slug, seq)
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no %s event of %s contains %q", typ, ref, contains)
	}
	return nil
}

// timelineListIs checks that the field of each item of the response's list holds
// the values, in order.
func (h *harness) timelineListIs(list, field, want string) error {
	got, ok, err := h.responsePath(list)
	if err != nil {
		return err
	}
	items, isList := got.([]any)
	if !ok || !isList {
		return fmt.Errorf("the response %s has no list %q", h.api.body, list)
	}
	parts := make([]string, len(items))
	for i, item := range items {
		obj, _ := item.(map[string]any)
		parts[i] = fmt.Sprint(obj[field])
	}
	if text := strings.Join(parts, ","); text != strings.Join(splitList(want), ",") {
		return fmt.Errorf("%s of %s are %q, want %q (response %s)", field, list, text, want, h.api.body)
	}
	return nil
}

// timelineMarkersAre checks the markers as task:type pairs, in order.
func (h *harness) timelineMarkersAre(want string) error {
	got, _, err := h.responsePath("markers")
	if err != nil {
		return err
	}
	items, _ := got.([]any)
	parts := make([]string, len(items))
	for i, item := range items {
		obj, _ := item.(map[string]any)
		parts[i] = fmt.Sprint(obj["task"], ":", obj["type"])
	}
	if text := strings.Join(parts, ","); text != strings.Join(splitList(want), ",") {
		return fmt.Errorf("the markers are %q, want %q (response %s)", text, want, h.api.body)
	}
	return nil
}

// timelineSpanIs checks the answered window is the given number of days long.
func (h *harness) timelineSpanIs(days int) error {
	var from, to time.Time
	for path, dst := range map[string]*time.Time{"from": &from, "to": &to} {
		v, ok, err := h.responsePath(path)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("the response %s has no %q", h.api.body, path)
		}
		t, err := time.Parse(time.RFC3339Nano, fmt.Sprint(v))
		if err != nil {
			return err
		}
		*dst = t
	}
	if got := to.Sub(from); got != time.Duration(days)*24*time.Hour {
		return fmt.Errorf("the window is %s long, want %d days", got, days)
	}
	return nil
}

func registerTimelineSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I request the timeline with "([^"]*)" as actor "([^"]*)"$`, h.getTimeline)
	sc.Step(`^I request the timeline for the window from (-?\d+) to (-?\d+) days with "([^"]*)" as actor "([^"]*)"$`, h.getTimelineWindow)
	sc.Step(`^the task "([^"]*)" was created (-?\d+) days ago$`, h.createdDaysAgo)
	sc.Step(`^the task "([^"]*)" is planned from (-?\d+) to (-?\d+) days from now$`, h.plannedBetween)
	sc.Step(`^the lease "([^"]*)" ran from (\d+) days ago to (\d+) days ago$`, h.leaseRan)
	sc.Step(`^the ([a-z_]+) events of the task "([^"]*)" containing "([^"]*)" happened (\d+) days ago$`, h.eventsHappened)
	sc.Step(`^the timeline ([a-z]+) have ([a-z_]+) "([^"]*)"$`, h.timelineListIs)
	sc.Step(`^the timeline markers are "([^"]*)"$`, h.timelineMarkersAre)
	sc.Step(`^the timeline window is (\d+) days long$`, h.timelineSpanIs)
}
