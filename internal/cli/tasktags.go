package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// tagMethods is the HTTP method of `vh task tag` (add) and `vh task untag`
// (remove), and the words of the `ok` line that follow the task's ref.
var tagMethods = map[string]struct{ method, phrase string }{
	"add":    {"PUT", "tagged"},
	"remove": {"DELETE", "untagged"},
}

// transitionTags is the subcommand that adds or removes the tags it is given.
func transitionTags(op string) func([]string, io.Writer, io.Writer, Env) int {
	return func(args []string, stdout, stderr io.Writer, env Env) int {
		return taskTagCmd(op, args, stdout, stderr, env)
	}
}

// taskTagCmd runs `vh task tag|untag <ref> <tag>...`: one request per tag, in
// the order given. It stops at the first refusal, so the tags before it stay
// changed and the ones after it are not sent.
func taskTagCmd(op string, args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, taskTagFlags)
	if !ok || len(p.pos) < 2 {
		return usageLine(stderr, TaskUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	m := tagMethods[op]
	ref := p.pos[0]
	var raw json.RawMessage
	for _, tag := range p.pos[1:] {
		path := "/api/v1/tasks/" + url.PathEscape(ref) + "/tags/" + url.PathEscape(tag)
		if err := c.do(ctx, m.method, path, nil, nil, &raw); err != nil {
			return report(stderr, tagFailure(err, ref))
		}
	}
	return printTaskWrite(p, raw, m.phrase+" "+strings.ToLower(strings.Join(p.pos[1:], " ")), stdout, stderr)
}

// tagFailure words the next action of a refused tag change: an unknown task
// sends the agent to task ls, a closed task or initiative to task show of it.
func tagFailure(err error, ref string) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.Status {
	case http.StatusNotFound:
		return withNext(err, ae.Status, "vh task ls")
	case http.StatusConflict:
		return conflictError(ae, "vh task show "+ref)
	}
	return err
}
