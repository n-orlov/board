package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// CatchupUsage is the single stderr line for a malformed `vh catchup`.
const CatchupUsage = "usage: vh catchup [--json]"

var catchupFlags = []flagSpec{{"--json", false}}

// catchupView is what POST /api/v1/catchup answers: the notes it acknowledged,
// how many more there are and the answers to my asks since the last catch-up.
type catchupView struct {
	Notes     []noteView `json:"notes"`
	NotesMore int        `json:"notes_more"`
	Answers   []askView  `json:"answers"`
}

// catchupCmd runs `vh catchup`: the notes after my notes cursor, acknowledged,
// and the answers to my asks since my previous catch-up, in one request. It
// prints the `vh notes` line of each note, then the `ask --poll` answer line of
// each answer, and nothing when there is nothing new.
func catchupCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, catchupFlags)
	if !ok || len(p.pos) > 0 {
		return usageLine(stderr, CatchupUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/catchup", nil, map[string]any{}, &raw); err != nil {
		return report(stderr, err)
	}
	var v catchupView
	if err := json.Unmarshal(raw, &v); err != nil {
		return report(stderr, err)
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
	} else {
		_, _ = fmt.Fprint(stdout, catchupText(v, nowFn()))
	}
	return ExitOK
}

// catchupText is one `vh notes` line for each note, then one `ok A<n> answered
// by <actor>: <answer>` line for each answer, then `+K more → vh catchup` when
// notes are left for the next call.
func catchupText(v catchupView, now time.Time) string {
	var b strings.Builder
	for _, line := range noteLines(v.Notes, now) {
		b.WriteString(line + "\n")
	}
	for _, a := range v.Answers {
		b.WriteString(pollLine(a) + "\n")
	}
	if v.NotesMore > 0 {
		fmt.Fprintf(&b, "+%d more → vh catchup\n", v.NotesMore)
	}
	return b.String()
}
