package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// depOps is what `board task dep add|rm` does by subcommand: the HTTP method
// and the words of the `ok` line that follow the dependent task's ref.
var depOps = map[string]struct{ method, phrase string }{
	"add": {"POST", "blocked by"},
	"rm":  {"DELETE", "no longer blocked by"},
}

// taskDep runs `board task dep add|rm <ref> <blocked-by-ref>`.
func taskDep(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		return usageLine(stderr, TaskUsage)
	}
	op, known := depOps[args[0]]
	p, ok := parseArgs(args[1:], taskDepFlags)
	if !known || !ok || len(p.pos) != 2 {
		return usageLine(stderr, TaskUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	ref, blocker := p.pos[0], p.pos[1]
	path := "/api/v1/tasks/" + url.PathEscape(ref) + "/deps/" + url.PathEscape(blocker)
	var raw json.RawMessage
	if err := c.do(ctx, op.method, path, nil, nil, &raw); err != nil {
		return report(stderr, depFailure(err, ref))
	}
	return printTaskWrite(p, raw, op.phrase+" "+strings.ToLower(blocker), stdout, stderr)
}

// depFailure words the next action of a refused dependency change. A
// dependency that does not exist and a cycle send the agent to task show of
// the dependent task, which lists its deps; an unknown task sends it to task
// ls. A cycle exits 6.
func depFailure(err error, ref string) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	show := "board task show " + strings.ToLower(ref)
	switch ae.Status {
	case http.StatusNotFound:
		if strings.Contains(ae.Detail, "is not blocked by") {
			return withNext(err, ae.Status, show)
		}
		return withNext(err, ae.Status, "board task ls")
	case http.StatusConflict:
		return conflictError(ae, show)
	}
	return err
}
