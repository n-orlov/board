package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// PingUsage is the single stderr line for a malformed `vh ping`.
const PingUsage = "usage: vh ping [\"<progress>\"] [--json]"

var pingFlags = []flagSpec{{"--json", false}}

// pingView is what POST /api/v1/ping answers: the leases it renewed and the
// refs of the tasks it wrote a milestone on.
type pingView struct {
	Leases     []leaseView `json:"leases"`
	Milestones []string    `json:"milestones"`
}

// pingCmd runs `vh ping ["<progress>"]`: every live lease of mine renewed and,
// with progress, a milestone on each leased task, in one request. It prints the
// `lease renew` line of each lease and, with progress, the `milestone` line of
// each task.
func pingCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, pingFlags)
	if !ok || len(p.pos) > 1 {
		return usageLine(stderr, PingUsage)
	}
	body := map[string]any{}
	if len(p.pos) == 1 {
		body["progress"] = p.pos[0]
	}
	return postPing(p, body, stdout, stderr, env)
}

// postPing sends the ping body and prints the answer.
func postPing(p parsedArgs, body map[string]any, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/ping", nil, body, &raw); err != nil {
		return report(stderr, pingFailure(err))
	}
	var v pingView
	if err := json.Unmarshal(raw, &v); err != nil {
		return report(stderr, err)
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
	} else {
		_, _ = fmt.Fprint(stdout, pingText(v))
	}
	return ExitOK
}

// pingText is the `ok lease` line of each renewed lease, then the `ok task`
// line of each task a milestone was written on.
func pingText(v pingView) string {
	var b strings.Builder
	for _, l := range v.Leases {
		b.WriteString("ok lease " + l.Ref + " " + l.Task + " exp " + leaseTTL(l) + "\n")
	}
	for _, t := range v.Milestones {
		b.WriteString("ok task " + t + " milestone recorded\n")
	}
	return b.String()
}

// pingFailure words the next action of a refused ping: a conflict exits 6 with
// the server's next.
func pingFailure(err error) error {
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == 409 {
		return conflictError(ae, ae.Next)
	}
	return err
}
