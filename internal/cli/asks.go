package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// AskUsage is the single stderr line for a malformed `board ask` command line.
const AskUsage = "usage: board ask <task> \"<question>\" [--details file|-] [--option a]... [--stop] [--blocking] [--json] | show <A…> [--json] | ls [--state open|answered|withdrawn|all] [--mine] [--task ref] [--limit N|--all] [--json] | withdraw <A…> [\"<reason>\"] [--json]"

// AnswerUsage is the single stderr line for a malformed `board answer` command line.
const AnswerUsage = "usage: board answer <A…> \"<answer>\" [--relay] [--for <handle>] [--json]"

// askStates are the values of --state of ask ls.
var askStates = []string{"open", "answered", "withdrawn", "all"}

// askView is the part of an ask the text output shows.
type askView struct {
	Ref            string   `json:"ref"`
	Task           string   `json:"task"`
	Initiative     string   `json:"initiative"`
	Asker          string   `json:"asker"`
	Question       string   `json:"question"`
	Details        string   `json:"details"`
	Options        []string `json:"options"`
	Mode           string   `json:"mode"`
	Blocking       bool     `json:"blocking"`
	State          string   `json:"state"`
	Answer         string   `json:"answer"`
	AnsweredBy     string   `json:"answered_by"`
	OnBehalfOf     string   `json:"on_behalf_of"`
	Channel        string   `json:"channel"`
	CreatedAt      string   `json:"created_at"`
	AnsweredAt     string   `json:"answered_at"`
	WithdrawnAt    string   `json:"withdrawn_at"`
	WithdrawReason string   `json:"withdraw_reason"`
}

// askSubcommands are the words after `board ask` and what runs each; any
// other first word starts a new ask.
var askSubcommands = map[string]func([]string, io.Writer, io.Writer, Env) int{
	"show":     askShow,
	"ls":       askLs,
	"withdraw": askWithdraw,
}

// askCmd runs `board ask ...` against the server named by BOARD_URL.
func askCmd(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) > 0 {
		if run, ok := askSubcommands[args[0]]; ok {
			return run(args[1:], stdout, stderr, env)
		}
	}
	return askRaise(args, stdout, stderr, env)
}

// askRaise runs `board ask <task> "<question>"`.
func askRaise(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, askFlags)
	if !ok || len(p.pos) != 2 {
		return usageLine(stderr, AskUsage)
	}
	body, err := raiseBody(p)
	if err != nil {
		return report(stderr, err)
	}
	return askWrite(p, "POST", "/api/v1/asks", body, raiseFailure(p.pos[0]), "raised", stdout, stderr, env)
}

// raiseBody builds the POST body of a new ask.
func raiseBody(p parsedArgs) (map[string]any, error) {
	body := map[string]any{"task": p.pos[0], "question": p.pos[1], "mode": "stop"}
	if src := last(p.values["--details"]); src != "" {
		text, err := readTextSource(src, "details")
		if err != nil {
			return nil, err
		}
		body["details"] = text
	}
	if opts := p.values["--option"]; len(opts) > 0 {
		body["options"] = opts
	}
	if p.has("--blocking") {
		body["blocking"] = true
	}
	return body, nil
}

// askWrite sends one ask write and prints `ok ask A<n> <verb>`, or the ask with --json.
func askWrite(p parsedArgs, method, path string, body any, failure func(error) error, verb string, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, method, path, nil, body, &raw); err != nil {
		return report(stderr, failure(err))
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var a askView
	if err := json.Unmarshal(raw, &a); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "ok ask %s %s\n", a.Ref, verb)
	return ExitOK
}

// raiseFailure words the next action of a refused ask: an unknown task exits 4
// with task ls, a closed task or initiative exits 6 with task show.
func raiseFailure(task string) func(error) error {
	return func(err error) error {
		var ae *apiError
		if !errors.As(err, &ae) {
			return err
		}
		switch ae.Status {
		case http.StatusNotFound:
			return withNext(err, ae.Status, "board task ls")
		case http.StatusConflict:
			return conflictError(ae, "board task show "+strings.ToLower(task))
		}
		return err
	}
}

// askRefFailure words the next action of a refused ask read or move: an
// unknown ask exits 4 with ask ls, an ask that is not open exits 6 with ask show.
func askRefFailure(ref string) func(error) error {
	return func(err error) error {
		var ae *apiError
		if !errors.As(err, &ae) {
			return err
		}
		switch ae.Status {
		case http.StatusNotFound:
			return withNext(err, ae.Status, "board ask ls")
		case http.StatusConflict:
			return conflictError(ae, "board ask show "+strings.ToUpper(ref))
		}
		return err
	}
}

// askPath is the path of the ask ref, with an action after it when given.
func askPath(ref, action string) string {
	path := "/api/v1/asks/" + url.PathEscape(ref)
	if action != "" {
		path += "/" + action
	}
	return path
}

// askWithdraw runs `board ask withdraw <A…> ["<reason>"]`.
func askWithdraw(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, askDropFlags)
	if !ok || len(p.pos) < 1 || len(p.pos) > 2 {
		return usageLine(stderr, AskUsage)
	}
	var body any
	if len(p.pos) == 2 {
		body = map[string]any{"reason": p.pos[1]}
	}
	return askWrite(p, "POST", askPath(p.pos[0], "withdraw"), body, askRefFailure(p.pos[0]), "withdrawn", stdout, stderr, env)
}

// answerCmd runs `board answer <A…> "<answer>" [--relay] [--for <handle>]`.
func answerCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, answerFlags)
	if !ok || len(p.pos) != 2 || (p.has("--for") && !p.has("--relay")) {
		return usageLine(stderr, AnswerUsage)
	}
	body := map[string]any{"answer": p.pos[1]}
	if p.has("--relay") {
		body["channel"] = "relay"
	}
	if who := last(p.values["--for"]); who != "" {
		body["on_behalf_of"] = who
	}
	return askWrite(p, "POST", askPath(p.pos[0], "answer"), body, askRefFailure(p.pos[0]), "answered", stdout, stderr, env)
}

// askShow runs `board ask show <A…>`.
func askShow(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, askShowFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, AskUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "GET", askPath(p.pos[0], ""), nil, nil, &raw); err != nil {
		return report(stderr, askRefFailure(p.pos[0])(err))
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var a askView
	if err := json.Unmarshal(raw, &a); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprint(stdout, askDetail(a, nowFn()))
	return ExitOK
}

// askQuery is what `board ask ls` asks the server for.
type askQuery struct {
	state, task string
	mine        bool
}

// askFilter reads the filters of ask ls. A state outside the four is not ok.
func askFilter(p parsedArgs) (askQuery, bool) {
	q := askQuery{state: last(p.values["--state"]), task: last(p.values["--task"]), mine: p.has("--mine")}
	return q, q.state == "" || slices.Contains(askStates, q.state)
}

// command is the `board ask ls` line that shows the same asks, with --all.
func (q askQuery) command() string {
	cmd := "board ask ls"
	if q.state != "" {
		cmd += " --state " + q.state
	}
	if q.mine {
		cmd += " --mine"
	}
	if q.task != "" {
		cmd += " --task " + quoteArg(q.task)
	}
	return cmd
}

// values are the query parameters of the list; actor is who --mine means.
func (q askQuery) values(actor string) url.Values {
	v := url.Values{}
	if q.state != "" {
		v.Set("state", q.state)
	}
	if q.task != "" {
		v.Set("task", q.task)
	}
	if q.mine {
		v.Set("actor", actor)
	}
	return v
}

// askListArgs reads the filters and the cap of ask ls; parsed says whether the flags parsed.
func askListArgs(p parsedArgs, parsed bool) (askQuery, int, bool) {
	limit, okCap := listCap(p)
	filter, okFilter := askFilter(p)
	return filter, limit, parsed && okCap && okFilter && len(p.pos) == 0
}

// askLs runs `board ask ls`.
func askLs(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, askLsFlags)
	filter, limit, ok := askListArgs(p, ok)
	if !ok {
		return usageLine(stderr, AskUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	items, err := c.allPages(ctx, "/api/v1/asks", filter.values(c.id.Actor))
	if err != nil {
		return report(stderr, withNext(err, http.StatusNotFound, "board task ls"))
	}
	items, more := capItems(items, limit)
	if p.has("--json") {
		return printJSON(stdout, stderr, items)
	}
	return printAskLines(stdout, stderr, items, more, filter.command())
}

func printAskLines(stdout, stderr io.Writer, items []json.RawMessage, more int, cmd string) int {
	now := nowFn()
	for _, raw := range items {
		var a askView
		if err := json.Unmarshal(raw, &a); err != nil {
			return report(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, askLine(a, now))
	}
	if more > 0 {
		_, _ = fmt.Fprintf(stdout, "+%d more → %s --all\n", more, cmd)
	}
	return ExitOK
}

// askLine is `A3  open  <task>  <asker>  "<question>"  (blocking, poll)  asked 3m ago`;
// an answered ask ends `answered by <h> 3m ago`, a withdrawn one `withdrawn 3m ago`.
// An ask on no task shows `-` for the task.
func askLine(a askView, now time.Time) string {
	task := a.Task
	if task == "" {
		task = "-"
	}
	line := strings.Join([]string{a.Ref, a.State, task, a.Asker, strconv.Quote(a.Question)}, "  ")
	if m := askMarks(a); m != "" {
		line += "  (" + m + ")"
	}
	return line + "  " + askEnding(a, now)
}

// askMarks lists the marks an ask carries: blocking, and poll mode.
func askMarks(a askView) string {
	var marks []string
	if a.Blocking {
		marks = append(marks, "blocking")
	}
	if a.Mode == "poll" {
		marks = append(marks, "poll")
	}
	return strings.Join(marks, ", ")
}

// askEnding says when the ask was raised, or how it ended.
func askEnding(a askView, now time.Time) string {
	switch a.State {
	case "answered":
		return "answered by " + a.AnsweredBy + " " + ago(now, a.AnsweredAt)
	case "withdrawn":
		return "withdrawn " + ago(now, a.WithdrawnAt)
	}
	return "asked " + ago(now, a.CreatedAt)
}

// askDetail is the `ask show` text: the one-line form, a line per fact, the
// options and details, then how it was answered or withdrawn.
func askDetail(a askView, now time.Time) string {
	var b strings.Builder
	b.WriteString(askLine(a, now) + "\n")
	if a.Initiative != "" {
		b.WriteString("initiative: " + a.Initiative + "\n")
	}
	for _, o := range a.Options {
		b.WriteString("option: " + o + "\n")
	}
	if a.Details != "" {
		b.WriteString(a.Details + "\n")
	}
	b.WriteString(askOutcome(a))
	return b.String()
}

// askOutcome is the answer, with the person it was relayed for, or the withdrawal reason.
func askOutcome(a askView) string {
	switch a.State {
	case "answered":
		via := a.Channel
		if a.OnBehalfOf != "" {
			via += " for " + a.OnBehalfOf
		}
		return "answer (" + via + "): " + a.Answer + "\n"
	case "withdrawn":
		if a.WithdrawReason != "" {
			return "reason: " + a.WithdrawReason + "\n"
		}
	}
	return ""
}
