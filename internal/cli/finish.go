package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FinishUsage is the single stderr line for a malformed `vh finish`.
const FinishUsage = "usage: vh finish [<task>] \"<outcome>\" [--done] [--json]"

var finishFlags = []flagSpec{{"--done", false}, {"--json", false}}

// finishView is what POST /api/v1/finish answers: the lease that ended and
// whether the task was marked done.
type finishView struct {
	Lease leaseView `json:"lease"`
	Done  bool      `json:"done"`
}

// finishCmd runs `vh finish [<task>] "<outcome>" [--done]`: the lease end, the
// outcome note and, with --done, the task done, in one request. It prints the
// `lease end` line and, with --done, the `task done` line.
func finishCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, finishFlags)
	if !ok || len(p.pos) < 1 || len(p.pos) > 2 {
		return usageLine(stderr, FinishUsage)
	}
	return postFinish(p, finishBody(p), stdout, stderr, env)
}

// finishBody is the request of a finish: the outcome (the last argument), the
// task (the first, when two are given) and done.
func finishBody(p parsedArgs) map[string]any {
	body := map[string]any{"outcome": p.pos[len(p.pos)-1], "done": p.has("--done")}
	if len(p.pos) == 2 {
		body["task"] = p.pos[0]
	}
	return body
}

// postFinish sends the finish body and prints the answer.
func postFinish(p parsedArgs, body map[string]any, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/finish", nil, body, &raw); err != nil {
		return report(stderr, finishFailure(err))
	}
	var v finishView
	if err := json.Unmarshal(raw, &v); err != nil {
		return report(stderr, err)
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
	} else {
		_, _ = fmt.Fprint(stdout, finishText(v))
	}
	return ExitOK
}

// finishText is the `ok lease` line of the lease that ended and, when the task
// was marked done, the `ok task` line.
func finishText(v finishView) string {
	out := "ok lease " + v.Lease.Ref + " ended\n"
	if v.Done {
		out += "ok task " + v.Lease.Task + " done\n"
	}
	return out
}

// finishFailure words the next action of a refused finish: an unknown task
// exits 4 with task ls, a conflict exits 6 with the server's next, except the
// done another actor's lease stops, which names `vh task done --force`.
func finishFailure(err error) error {
	var ae *apiError
	if !errors.As(err, &ae) {
		return err
	}
	switch ae.Status {
	case http.StatusNotFound:
		return withNext(err, ae.Status, "vh task ls")
	case http.StatusConflict:
		if f := strings.Fields(ae.Detail); strings.HasPrefix(ae.Detail, "cannot mark task ") && len(f) > 3 {
			return conflictError(ae, "wait for the other lease to end, or vh task done "+f[3]+" --force")
		}
		return conflictError(ae, ae.Next)
	}
	return err
}
