package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// showEvents is how many of a task's latest events `task show` prints.
const showEvents = 10

// task is the part of a task the text output shows.
type task struct {
	Ref            string      `json:"ref"`
	Initiative     string      `json:"initiative"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	Kind           string      `json:"kind"`
	Status         string      `json:"status"`
	Resolution     string      `json:"resolution"`
	PlannedStart   string      `json:"planned_start"`
	PlannedEnd     string      `json:"planned_end"`
	CadenceSeconds int64       `json:"cadence_seconds"`
	NextCheckAt    string      `json:"next_check_at"`
	ExternalRefs   []extRef    `json:"external_refs"`
	CreatedBy      string      `json:"created_by"`
	CreatedAt      string      `json:"created_at"`
	DoneAt         string      `json:"done_at"`
	DoneBy         string      `json:"done_by"`
	ClosedAt       string      `json:"closed_at"`
	ClosedBy       string      `json:"closed_by"`
	Active         bool        `json:"active"`
	Blocked        bool        `json:"blocked"`
	Overdue        bool        `json:"overdue"`
	Deps           []taskLink  `json:"deps"`
	Blocking       []taskLink  `json:"blocking"`
	LiveLeases     []leaseView `json:"live_leases"`
}

type taskLink struct {
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type leaseView struct {
	Ref       string   `json:"ref"`
	Task      string   `json:"task"`
	Actor     string   `json:"actor"`
	Intent    string   `json:"intent"`
	State     string   `json:"state"`
	Resources []string `json:"resources"`
	RenewedAt string   `json:"renewed_at"`
	ExpiresAt string   `json:"expires_at"`
	EndedAt   string   `json:"ended_at"`
}

type eventView struct {
	Ref     string `json:"ref"`
	At      string `json:"at"`
	Actor   string `json:"actor"`
	Summary string `json:"summary"`
}

// taskLine is the one-line form: ref, status, title and the marks it has.
func taskLine(t task) string {
	line := t.Ref + "  " + t.Status + "  " + t.Title
	var marks []string
	for _, m := range []struct {
		on   bool
		name string
	}{{t.Kind == "watch", "watch"}, {t.Blocked, "blocked"}, {t.Active, "active"}, {t.Overdue, "overdue"}} {
		if m.on {
			marks = append(marks, m.name)
		}
	}
	if len(marks) > 0 {
		line += "  (" + strings.Join(marks, ", ") + ")"
	}
	return line
}

func taskShow(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, taskShowFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, TaskUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "GET", "/api/v1/tasks/"+url.PathEscape(p.pos[0]), nil, nil, &raw); err != nil {
		return report(stderr, withNext(err, http.StatusNotFound, "board task ls"))
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	text, err := c.taskText(ctx, raw, p.pos[0])
	if err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprint(stdout, text)
	return ExitOK
}

// taskText is the `task show` text of the task document raw and its last events.
func (c *client) taskText(ctx context.Context, raw json.RawMessage, ref string) (string, error) {
	var t task
	if err := json.Unmarshal(raw, &t); err != nil {
		return "", err
	}
	events, err := c.lastEvents(ctx, ref, showEvents)
	if err != nil {
		return "", err
	}
	return taskDetail(t, events, nowFn()), nil
}

// lastEvents reads a task's events page by page and keeps the last n.
func (c *client) lastEvents(ctx context.Context, ref string, n int) ([]eventView, error) {
	var events []eventView
	after := ""
	for {
		q := url.Values{"limit": {strconv.Itoa(listPageSize)}}
		if after != "" {
			q.Set("after", after)
		}
		var page struct {
			Items     []eventView `json:"items"`
			NextAfter string      `json:"next_after"`
		}
		if err := c.do(ctx, "GET", "/api/v1/tasks/"+url.PathEscape(ref)+"/events", q, nil, &page); err != nil {
			return nil, err
		}
		events = append(events, page.Items...)
		if page.NextAfter == "" || page.NextAfter == after {
			break
		}
		after = page.NextAfter
	}
	if len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}

// taskDetail is the `task show` text: the one-line form, a line per fact the
// task has, its dependencies, live leases and the description, then the events.
func taskDetail(t task, events []eventView, now time.Time) string {
	var b strings.Builder
	b.WriteString(taskLine(t) + "\n")
	b.WriteString("initiative: " + t.Initiative + "\n")
	b.WriteString(taskFacts(t, now))
	for _, d := range t.Deps {
		b.WriteString("blocked by " + linkLine(d) + "\n")
	}
	for _, d := range t.Blocking {
		b.WriteString("blocking " + linkLine(d) + "\n")
	}
	for _, l := range t.LiveLeases {
		b.WriteString("lease " + l.Ref + " by " + l.Actor + ": " + l.Intent + " (expires " + untilTime(now, l.ExpiresAt) + ")\n")
	}
	if t.Description != "" {
		b.WriteString(t.Description + "\n")
	}
	if len(events) > 0 {
		b.WriteString("events:\n")
	}
	for _, e := range events {
		b.WriteString("  " + eventLine(now, e) + "\n")
	}
	return b.String()
}

func linkLine(l taskLink) string { return l.Ref + " (" + l.Status + ") " + l.Title }

// taskFacts is the lines for the dates, cadence, links and who did what.
func taskFacts(t task, now time.Time) string {
	var b strings.Builder
	if t.PlannedStart != "" || t.PlannedEnd != "" {
		b.WriteString("planned: " + shortTime(t.PlannedStart) + " → " + shortTime(t.PlannedEnd) + "\n")
	}
	if t.CadenceSeconds > 0 {
		b.WriteString("cadence: " + formatDuration(t.CadenceSeconds) + ", next check " + untilTime(now, t.NextCheckAt) + "\n")
	}
	for _, r := range t.ExternalRefs {
		b.WriteString("link: " + r.Label + " " + r.URL + "\n")
	}
	b.WriteString("created by " + t.CreatedBy + " " + ago(now, t.CreatedAt) + "\n")
	if t.DoneAt != "" {
		b.WriteString("done by " + t.DoneBy + " " + ago(now, t.DoneAt) + "\n")
	}
	if t.ClosedAt != "" {
		b.WriteString("closed (" + t.Resolution + ") by " + t.ClosedBy + " " + ago(now, t.ClosedAt) + "\n")
	}
	return b.String()
}

// shortTime is the date of a midnight-UTC time and the RFC 3339 time otherwise.
func shortTime(stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	if t.UTC().Format("15:04:05") == "00:00:00" {
		return t.UTC().Format(time.DateOnly)
	}
	return t.UTC().Format(time.RFC3339)
}

// untilTime renders an RFC 3339 time relative to now: in 5m, in 2h, 3d ago.
func untilTime(now time.Time, stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	if !t.After(now) {
		return ago(now, stamp)
	}
	d := t.Sub(now)
	switch {
	case d < time.Minute:
		return "in under 1m"
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("in %dd", int(d/(24*time.Hour)))
	}
}
