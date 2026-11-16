package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// StartUsage is the single stderr line for a malformed `vh start`.
const StartUsage = "usage: vh start <task> \"<intent>\" [--res r]... [--ttl D] [--json]"

// NewUsage is the single stderr line for a malformed `vh new`.
const NewUsage = "usage: vh new <initiative> \"<title>\" --start \"<intent>\" [--res r]... [--ttl D] [--json]"

var (
	startFlags = []flagSpec{{"--res", true}, {"--ttl", true}, {"--json", false}}
	newFlags   = []flagSpec{{"--start", true}, {"--res", true}, {"--ttl", true}, {"--json", false}}
)

// startView is what POST /api/v1/start answers: the lease and its conflicts,
// whether the request created the task, and the caller's check-in.
type startView struct {
	leaseResult
	TaskCreated bool    `json:"task_created"`
	Now         nowView `json:"now"`
}

// startCmd runs `vh start <task> "<intent>"`: the lease start and the check-in
// in one request. It prints the `lease start` lines and then the now summary,
// and exits 7 when the lease conflicts with another's.
func startCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, startFlags)
	if !ok || len(p.pos) != 2 {
		return usageLine(stderr, StartUsage)
	}
	body, err := leaseBody(p)
	if err != nil {
		return badFlagValue(stderr, err)
	}
	body["task"], body["intent"] = p.pos[0], p.pos[1]
	return postStart(p, body, func(err error) error { return startFailure(err, p.pos[0]) }, stdout, stderr, env)
}

// newCmd runs `vh new <initiative> "<title>" --start "<intent>"`: the task
// creation, the lease start and the check-in in one request. It prints the
// `task add` line, the `lease start` lines and the now summary.
func newCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, newFlags)
	intent := last(p.values["--start"])
	if !ok || len(p.pos) != 2 || intent == "" {
		return usageLine(stderr, NewUsage)
	}
	body, err := leaseBody(p)
	if err != nil {
		return badFlagValue(stderr, err)
	}
	body["new_task"] = map[string]any{"initiative": p.pos[0], "title": p.pos[1]}
	body["intent"] = intent
	return postStart(p, body, func(err error) error { return taskWriteFailure(err, p.pos[0], "POST") }, stdout, stderr, env)
}

// postStart sends the start body, adding the resources, and prints the answer.
// failure words the next action of a refusal.
func postStart(p parsedArgs, body map[string]any, failure func(error) error, stdout, stderr io.Writer, env Env) int {
	if res := p.values["--res"]; len(res) > 0 {
		body["resources"] = res
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/start", nil, body, &raw); err != nil {
		return report(stderr, failure(err))
	}
	var v startView
	if err := json.Unmarshal(raw, &v); err != nil {
		return report(stderr, err)
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
	} else {
		_, _ = fmt.Fprint(stdout, startText(v, nowFn()))
	}
	if len(v.Conflicts) > 0 {
		return ExitWarning
	}
	return ExitOK
}

// startText is the `ok task` line of a task the start created, the `ok lease`
// line with a CONFLICT line for each conflict, and the now summary.
func startText(v startView, now time.Time) string {
	created := ""
	if v.TaskCreated {
		created = "ok task " + v.Lease.Task + " created\n"
	}
	return created + leaseResultText(v.leaseResult, now) + nowText(v.Now, now)
}
