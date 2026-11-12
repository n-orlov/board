package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// resolutions are the four ways a task closes; completed is the default.
var resolutions = []string{"completed", "dropped", "superseded", "duplicate"}

// operatorActor is the one actor `board task close --for` names.
const operatorActor = "operator"

// transitionVerbs is the word the `ok` line ends with, by command.
var transitionVerbs = map[string]string{"done": "done", "reopen": "reopened", "close": "closed"}

// transitionFlags is the flag table of a transition command.
func transitionFlags(cmd string) []flagSpec {
	switch cmd {
	case "done":
		return taskDoneFlags
	case "reopen":
		return taskReopenFlags
	default:
		return taskCloseFlags
	}
}

// closeBody reads --resolution (default completed) and --for. A resolution
// outside the four, or a --for other than operator, is an error worded for the
// flag.
func closeBody(p parsedArgs) (map[string]any, string, error) {
	res := last(p.values["--resolution"])
	if res == "" {
		res = "completed"
	}
	if !slices.Contains(resolutions, res) {
		return nil, "", fmt.Errorf("--resolution: invalid resolution %q: want completed, dropped, superseded or duplicate", res)
	}
	who := last(p.values["--for"])
	if who != "" && who != operatorActor {
		return nil, "", fmt.Errorf("--for: invalid actor %q: want operator", who)
	}
	return map[string]any{"resolution": res}, who, nil
}

// taskTransition runs `board task done|reopen|close <ref>`.
func taskTransition(cmd string, args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, transitionFlags(cmd))
	if !ok || !transitionArity(cmd, len(p.pos)) {
		return usageLine(stderr, TaskUsage)
	}
	var body any
	who := ""
	switch cmd {
	case "close":
		b, w, err := closeBody(p)
		if err != nil {
			return badFlagValue(stderr, err)
		}
		body, who = b, w
	case "done":
		body = doneBody(p)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	c.onBehalf = who
	ctx, stop := clientContext()
	defer stop()
	ref := p.pos[0]
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/tasks/"+url.PathEscape(ref)+"/"+cmd, nil, body, &raw); err != nil {
		return report(stderr, transitionFailure(err, cmd, ref))
	}
	return printTaskWrite(p, raw, transitionVerbs[cmd], stdout, stderr)
}

// transitionFailure words the next action of a refused transition: an unknown
// task exits 4 with task ls, a status that does not allow the move exits 6
// with task show.
func transitionFailure(err error, cmd, ref string) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.Status {
	case http.StatusNotFound:
		return withNext(err, ae.Status, "board task ls")
	case http.StatusConflict:
		if cmd == "done" && strings.HasPrefix(ae.Next, "wait for L") {
			return conflictError(ae, ae.Next)
		}
		return conflictError(ae, "board task show "+ref)
	}
	return err
}

// transitionArity is whether a transition takes that many positional arguments:
// the ref, and for done an optional outcome after it.
func transitionArity(cmd string, n int) bool {
	return n == 1 || (cmd == "done" && n == 2)
}

// doneBody is the POST body of task done: the outcome when one is given and
// force when --force is. Neither given sends no body.
func doneBody(p parsedArgs) map[string]any {
	body := map[string]any{}
	if len(p.pos) == 2 {
		body["outcome"] = p.pos[1]
	}
	if p.has("--force") {
		body["force"] = true
	}
	if len(body) == 0 {
		return nil
	}
	return body
}
