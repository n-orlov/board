package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// TaskUsage is the single stderr line for a malformed `board task` command line.
const TaskUsage = "usage: board task add <initiative> \"<title>\" [--description file|-] [--kind work|watch] [--cadence D] [--after ref]... [--start T] [--end T] [--link label=url]... [--json] | ls [<initiative>] [--status open|done|closed|not_closed|all] [--kind work|watch] [--active] [--blocked] [--overdue] [--q text] [--limit N|--all] [--json] | show <ref> [--json] | edit <ref> [--title T] [--description file|-] [--start T] [--end T] [--cadence D] [--link label=url]... [--no-links] [--json] | done <ref> [--json] | reopen <ref> [--json] | close <ref> [--resolution completed|dropped|superseded|duplicate] [--for operator] [--json]"

// taskCmd runs `board task ...` against the server named by BOARD_URL.
func taskCmd(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		return usageLine(stderr, TaskUsage)
	}
	switch args[0] {
	case "add":
		return taskAdd(args[1:], stdout, stderr, env)
	case "ls":
		return taskLs(args[1:], stdout, stderr, env)
	case "show":
		return taskShow(args[1:], stdout, stderr, env)
	case "edit":
		return taskEdit(args[1:], stdout, stderr, env)
	case "done", "reopen", "close":
		return taskTransition(args[0], args[1:], stdout, stderr, env)
	default:
		return usageLine(stderr, TaskUsage)
	}
}

// badFlagValue reports a flag value the CLI refuses before it asks the server.
func badFlagValue(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "board: %v\n", err)
	return ExitUsage
}

// descriptionFlag reads --description: nil when the flag is not given.
func descriptionFlag(p parsedArgs) (*string, error) {
	src := last(p.values["--description"])
	if src == "" {
		return nil, nil
	}
	text, err := readDescription(src)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

// addBody builds the POST body of task add.
func addBody(p parsedArgs, description *string) (map[string]any, error) {
	body := map[string]any{"initiative": p.pos[0], "title": p.pos[1]}
	if description != nil {
		body["description"] = *description
	}
	if k := last(p.values["--kind"]); k != "" {
		if k != "work" && k != "watch" {
			return nil, fmt.Errorf("--kind: invalid kind %q: want work or watch", k)
		}
		body["kind"] = k
	}
	if after := p.values["--after"]; len(after) > 0 {
		body["blocked_by"] = after
	}
	return body, commonFlags(p, body, false)
}

func taskAdd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, taskAddFlags)
	if !ok || len(p.pos) != 2 {
		return usageLine(stderr, TaskUsage)
	}
	description, err := descriptionFlag(p)
	if err != nil {
		return report(stderr, err)
	}
	body, err := addBody(p, description)
	if err != nil {
		return badFlagValue(stderr, err)
	}
	return writeTask(p, "POST", "/api/v1/tasks", body, "created", stdout, stderr, env)
}

// taskEditBody builds the PATCH body of task edit. No field to change, or --link with
// --no-links, is errBadUsage.
func taskEditBody(p parsedArgs, description *string) (map[string]any, error) {
	body := map[string]any{}
	if v := p.values["--title"]; len(v) > 0 {
		body["title"] = last(v)
	}
	if description != nil {
		body["description"] = *description
	}
	if p.has("--no-links") && len(p.values["--link"]) > 0 {
		return nil, errBadUsage
	}
	if err := commonFlags(p, body, p.has("--no-links")); err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errBadUsage
	}
	return body, nil
}

// errBadUsage is a task edit command line that asks for nothing, or for a contradiction.
var errBadUsage = errors.New("bad usage")

func taskEdit(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, taskEditFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, TaskUsage)
	}
	description, err := descriptionFlag(p)
	if err != nil {
		return report(stderr, err)
	}
	body, err := taskEditBody(p, description)
	if errors.Is(err, errBadUsage) {
		return usageLine(stderr, TaskUsage)
	}
	if err != nil {
		return badFlagValue(stderr, err)
	}
	return writeTask(p, "PATCH", "/api/v1/tasks/"+url.PathEscape(p.pos[0]), body, "updated", stdout, stderr, env)
}

// writeTask sends one create or edit and prints the `ok` line, or the object with --json.
func writeTask(p parsedArgs, method, path string, body any, verb string, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, method, path, nil, body, &raw); err != nil {
		return report(stderr, taskWriteFailure(err, p.pos[0], method))
	}
	return printTaskWrite(p, raw, verb, stdout, stderr)
}

// printTaskWrite prints the `ok` line of a task write, or the object with --json.
func printTaskWrite(p parsedArgs, raw json.RawMessage, verb string, stdout, stderr io.Writer) int {
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var t task
	if err := json.Unmarshal(raw, &t); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "ok task %s %s\n", t.Ref, verb)
	return ExitOK
}

// taskWriteFailure words the next action of a refused create or edit. For a
// create, arg is the initiative; for an edit it is the task ref. A 404 exits 4
// and a 409 (a closed task or initiative) exits 6.
func taskWriteFailure(err error, arg, method string) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	if method == "POST" {
		return addFailure(ae, err, arg)
	}
	return editFailure(ae, err, arg)
}

// addFailure: an unknown initiative sends the agent to init ls, an unknown
// blocker to the initiative's tasks, a closed initiative to init show.
func addFailure(ae *apiError, err error, initiative string) error {
	switch ae.Status {
	case http.StatusNotFound:
		if strings.HasPrefix(ae.Detail, "there is no initiative") {
			return withNext(err, ae.Status, "board init ls")
		}
		return withNext(err, ae.Status, "board task ls "+initiative)
	case http.StatusConflict:
		return conflictError(ae, "board init show "+initiative)
	}
	return err
}

func editFailure(ae *apiError, err error, ref string) error {
	switch ae.Status {
	case http.StatusNotFound:
		return withNext(err, ae.Status, "board task ls")
	case http.StatusConflict:
		return conflictError(ae, "board task show "+ref)
	}
	return err
}

func conflictError(ae *apiError, next string) error {
	return &apiError{Status: ae.Status, Detail: ae.Detail, Next: next, Exit: ExitConflict}
}

// taskQuery is what `board task ls` asks the server for.
type taskQuery struct {
	initiative string
	status     string
	kind       string
	flags      []string // active, blocked, overdue
	q          string
}

// taskFilter reads the filter flags and the optional initiative. A status or
// kind the server does not know is not ok.
func taskFilter(p parsedArgs) (taskQuery, bool) {
	q := taskQuery{status: last(p.values["--status"]), kind: last(p.values["--kind"]), q: last(p.values["--q"])}
	if len(p.pos) > 1 {
		return q, false
	}
	if len(p.pos) == 1 {
		q.initiative = p.pos[0]
	}
	q.flags = derivedFlags(p)
	return q, q.valid()
}

// derivedFlags is the names of --active, --blocked and --overdue that were given.
func derivedFlags(p parsedArgs) []string {
	var names []string
	for _, f := range []string{"active", "blocked", "overdue"} {
		if p.has("--" + f) {
			names = append(names, f)
		}
	}
	return names
}

func (q taskQuery) valid() bool {
	okStatus := q.status == "" || slices.Contains([]string{"open", "done", "closed", "not_closed", "all"}, q.status)
	return okStatus && (q.kind == "" || q.kind == "work" || q.kind == "watch")
}

func (q taskQuery) values() url.Values {
	v := url.Values{}
	if q.initiative != "" {
		v.Set("initiative", q.initiative)
	}
	for k, val := range map[string]string{"status": q.status, "kind": q.kind, "q": q.q} {
		if val != "" {
			v.Set(k, val)
		}
	}
	for _, f := range q.flags {
		v.Set(f, "true")
	}
	return v
}

// command is the `board task ls` line that shows the same tasks, with --all.
func (q taskQuery) command() string {
	cmd := "board task ls"
	if q.initiative != "" {
		cmd += " " + q.initiative
	}
	if q.status != "" {
		cmd += " --status " + q.status
	}
	if q.kind != "" {
		cmd += " --kind " + q.kind
	}
	for _, f := range q.flags {
		cmd += " --" + f
	}
	if q.q != "" {
		cmd += " --q " + quoteArg(q.q)
	}
	return cmd
}

// quoteArg wraps a word that a shell would split in double quotes.
func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t\"'$\\`") {
		return strconv.Quote(s)
	}
	return s
}

func taskLs(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, taskLsFlags)
	limit, okCap := listCap(p)
	filter, okFilter := taskFilter(p)
	if !ok || !okCap || !okFilter {
		return usageLine(stderr, TaskUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	items, err := c.allTasks(ctx, filter)
	if err != nil {
		return report(stderr, withNext(err, http.StatusNotFound, "board init ls"))
	}
	items, more := capItems(items, limit)
	if p.has("--json") {
		return printJSON(stdout, stderr, items)
	}
	return printTaskLines(stdout, stderr, items, more, filter.command())
}

// allTasks follows next_after until the last page.
func (c *client) allTasks(ctx context.Context, f taskQuery) ([]json.RawMessage, error) {
	items := []json.RawMessage{}
	after := ""
	for {
		q := f.values()
		q.Set("limit", strconv.Itoa(listPageSize))
		if after != "" {
			q.Set("after", after)
		}
		var page initiativePage
		if err := c.do(ctx, "GET", "/api/v1/tasks", q, nil, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextAfter == "" || page.NextAfter == after {
			return items, nil
		}
		after = page.NextAfter
	}
}

func printTaskLines(stdout, stderr io.Writer, items []json.RawMessage, more int, cmd string) int {
	for _, raw := range items {
		var t task
		if err := json.Unmarshal(raw, &t); err != nil {
			return report(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, taskLine(t))
	}
	if more > 0 {
		_, _ = fmt.Fprintf(stdout, "+%d more → %s --all\n", more, cmd)
	}
	return ExitOK
}
