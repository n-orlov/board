package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MilestoneUsage, CheckUsage and EventsUsage are the stderr lines for a
// malformed command line.
const (
	MilestoneUsage = "usage: vh milestone <task> \"<summary>\" [--json]"
	CheckUsage     = "usage: vh check <task> \"<summary>\" [--next D] [--json]"
	EventsUsage    = "usage: vh events [--since E<n>|time] [--task ref] [--initiative slug] [--type t]... [--actor handle] [--limit N|--all] [--json]"
)

// milestoneCmd runs `vh milestone <task> "<summary>"`.
func milestoneCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, milestoneFlags)
	if !ok || len(p.pos) != 2 {
		return usageLine(stderr, MilestoneUsage)
	}
	body := map[string]any{"summary": p.pos[1]}
	return writeTaskNote(p, "milestones", body, "milestone recorded", stdout, stderr, env)
}

// checkCmd runs `vh check <task> "<summary>" [--next D]`.
func checkCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, checkFlags)
	if !ok || len(p.pos) != 2 {
		return usageLine(stderr, CheckUsage)
	}
	body := map[string]any{"summary": p.pos[1]}
	if next := last(p.values["--next"]); next != "" {
		seconds, err := parseDuration(next)
		if err != nil {
			return badFlagValue(stderr, fmt.Errorf("--next: %w", err))
		}
		body["next_check_at"] = nowFn().Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
	}
	return writeTaskNote(p, "checks", body, "checked", stdout, stderr, env)
}

// writeTaskNote posts a milestone or a check to the task named first and
// prints `ok task <ref> <verb>`, or the task with --json.
func writeTaskNote(p parsedArgs, noun string, body map[string]any, verb string, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	ref := p.pos[0]
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/tasks/"+url.PathEscape(ref)+"/"+noun, nil, body, &raw); err != nil {
		return report(stderr, noteFailure(err, ref))
	}
	return printTaskWrite(p, raw, verb, stdout, stderr)
}

// noteFailure words the next action of a refused milestone or check: an
// unknown task sends the agent to task ls, a check on a work task or a closed
// watch task (409) to task show, with exit 6.
func noteFailure(err error, ref string) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.Status {
	case http.StatusNotFound:
		return withNext(err, ae.Status, "vh task ls")
	case http.StatusConflict:
		return conflictError(ae, "vh task show "+strings.ToLower(ref))
	}
	return err
}

// eventLine is the one-line form of an event: ref, relative time, actor, summary.
func eventLine(now time.Time, e eventView) string {
	return e.Ref + "  " + ago(now, e.At) + "  " + e.Actor + "  " + e.Summary
}

// eventQuery is what `vh events` asks the server for.
type eventQuery struct {
	since, task, initiative, actor string
	types                          []string
}

var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// eventFilter reads the filters. A --since given as a date is sent as the
// RFC 3339 time of its midnight UTC; an event ref, an id or a time goes as is.
func eventFilter(p parsedArgs) (eventQuery, error) {
	q := eventQuery{
		since: last(p.values["--since"]), task: last(p.values["--task"]),
		initiative: last(p.values["--initiative"]), actor: last(p.values["--actor"]),
		types: p.values["--type"],
	}
	if dateOnly.MatchString(q.since) {
		t, err := parseTime(q.since)
		if err != nil {
			return q, fmt.Errorf("--since: %w", err)
		}
		q.since = t
	}
	return q, nil
}

func (q eventQuery) values() url.Values {
	v := url.Values{}
	for k, val := range map[string]string{"since": q.since, "task": q.task, "initiative": q.initiative, "actor": q.actor} {
		if val != "" {
			v.Set(k, val)
		}
	}
	for _, t := range q.types {
		v.Add("type", t)
	}
	return v
}

// command is the `vh events` line that shows the same events, with --all.
func (q eventQuery) command() string {
	cmd := "vh events"
	for _, f := range []struct{ flag, val string }{{"since", q.since}, {"task", q.task}, {"initiative", q.initiative}, {"actor", q.actor}} {
		if f.val != "" {
			cmd += " --" + f.flag + " " + quoteArg(f.val)
		}
	}
	for _, t := range q.types {
		cmd += " --type " + quoteArg(t)
	}
	return cmd
}

// eventsCmd runs `vh events`.
func eventsCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, eventsFlags)
	limit, okCap := listCap(p)
	if !ok || !okCap || len(p.pos) != 0 {
		return usageLine(stderr, EventsUsage)
	}
	q, err := eventFilter(p)
	if err != nil {
		return badFlagValue(stderr, err)
	}
	return listEvents(p, "/api/v1/events", q.values(), limit, q.command(), stdout, stderr, env)
}

// taskEvents runs `vh task events <ref>`.
func taskEvents(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, taskEventsFlags)
	limit, okCap := listCap(p)
	if !ok || !okCap || len(p.pos) != 1 {
		return usageLine(stderr, TaskUsage)
	}
	ref := strings.ToLower(p.pos[0])
	path := "/api/v1/tasks/" + url.PathEscape(p.pos[0]) + "/events"
	return listEvents(p, path, url.Values{}, limit, "vh task events "+ref, stdout, stderr, env)
}

// listEvents prints the events at path, oldest first, capped at limit (0 for
// all) with a `+K more` line naming cmd, or the shown events as a JSON array.
func listEvents(p parsedArgs, path string, q url.Values, limit int, cmd string, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	items, err := c.allPages(ctx, path, q)
	if err != nil {
		return report(stderr, eventsFailure(err))
	}
	items, more := capItems(items, limit)
	if p.has("--json") {
		return printJSON(stdout, stderr, items)
	}
	now := nowFn()
	for _, raw := range items {
		var e eventView
		if err := json.Unmarshal(raw, &e); err != nil {
			return report(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, eventLine(now, e))
	}
	if more > 0 {
		_, _ = fmt.Fprintf(stdout, "+%d more → %s --all\n", more, cmd)
	}
	return ExitOK
}

// eventsFailure sends an unknown initiative to init ls and an unknown task to task ls.
func eventsFailure(err error) error {
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound && strings.HasPrefix(ae.Detail, "there is no initiative") {
		return withNext(err, ae.Status, "vh init ls")
	}
	return withNext(err, http.StatusNotFound, "vh task ls")
}

// allPages follows next_after until the last page of the list at path.
func (c *client) allPages(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	items := []json.RawMessage{}
	after := ""
	for {
		q.Set("limit", strconv.Itoa(listPageSize))
		if after != "" {
			q.Set("after", after)
		}
		var page initiativePage
		if err := c.do(ctx, "GET", path, q, nil, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextAfter == "" || page.NextAfter == after {
			return items, nil
		}
		after = page.NextAfter
	}
}
