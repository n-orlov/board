package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AttentionUsage is the single stderr line for a malformed `vh attention`.
const AttentionUsage = "usage: vh attention [--tag t]... [--init slug] [--limit N|--all] [--json]"

// DefaultAttentionCap is how many items each section of `vh attention` shows (SPEC §8.1).
const DefaultAttentionCap = 5

// attentionActor is an actor of an item and the way back the board has for it.
type attentionActor struct {
	Handle  string `json:"handle"`
	WayBack string `json:"way_back"`
}

// String is the handle, then the way back in parentheses when there is one.
func (a attentionActor) String() string {
	if a.WayBack == "" {
		return a.Handle
	}
	return a.Handle + " (" + a.WayBack + ")"
}

type attentionAsk struct {
	Ref          string         `json:"ref"`
	Question     string         `json:"question"`
	Task         string         `json:"task"`
	Asker        attentionActor `json:"asker"`
	Mode         string         `json:"mode"`
	AskerWaiting bool           `json:"asker_waiting"`
	Blocking     bool           `json:"blocking"`
	CreatedAt    string         `json:"created_at"`
}

type attentionLastLease struct {
	Lease   string `json:"lease"`
	Outcome string `json:"outcome"`
}

type attentionDone struct {
	Task      string              `json:"task"`
	Title     string              `json:"title"`
	DoneBy    attentionActor      `json:"done_by"`
	DoneAt    string              `json:"done_at"`
	LastLease *attentionLastLease `json:"last_lease"`
}

type attentionExpired struct {
	Lease     string         `json:"lease"`
	Holder    attentionActor `json:"holder"`
	Task      string         `json:"task"`
	Intent    string         `json:"intent"`
	RenewedAt string         `json:"renewed_at"`
}

type attentionLastCheck struct {
	Summary string         `json:"summary"`
	By      attentionActor `json:"by"`
	At      string         `json:"at"`
}

type attentionWatch struct {
	Task        string              `json:"task"`
	Title       string              `json:"title"`
	Owner       attentionActor      `json:"owner"`
	NextCheckAt string              `json:"next_check_at"`
	LastCheck   *attentionLastCheck `json:"last_check"`
}

type attentionHolder struct {
	Lease string         `json:"lease"`
	Actor attentionActor `json:"actor"`
	Task  string         `json:"task"`
}

type attentionConflict struct {
	Resource string            `json:"resource"`
	Holders  []attentionHolder `json:"holders"`
}

// attentionView is what GET /api/v1/attention answers.
type attentionView struct {
	OpenAsks       []attentionAsk      `json:"open_asks"`
	DoneNotClosed  []attentionDone     `json:"done_not_closed"`
	ExpiredLeases  []attentionExpired  `json:"expired_leases"`
	OverdueWatches []attentionWatch    `json:"overdue_watches"`
	Conflicts      []attentionConflict `json:"conflicts"`
}

// attentionCap reads --limit and --all like the other lists, with a cap of 5 by default.
func attentionCap(p parsedArgs) (int, bool) {
	if len(p.values["--limit"]) == 0 && !p.has("--all") {
		return DefaultAttentionCap, true
	}
	return listCap(p)
}

// attentionCmd runs `vh attention`: the five sections of what needs the operator.
func attentionCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, attentionFlags)
	limit, okCap := attentionCap(p)
	if !ok || !okCap || len(p.pos) != 0 {
		return usageLine(stderr, AttentionUsage)
	}
	raw, err := fetchAttention(p, env)
	if err != nil {
		return report(stderr, err)
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var v attentionView
	if err := json.Unmarshal(raw, &v); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprint(stdout, attentionText(v, nowFn(), limit, attentionCommand(p)))
	return ExitOK
}

// fetchAttention asks the server for the sections the filters of p select.
func fetchAttention(p parsedArgs, env Env) (json.RawMessage, error) {
	c, err := newClient(env)
	if err != nil {
		return nil, err
	}
	ctx, stop := clientContext()
	defer stop()
	q := url.Values{}
	for _, t := range p.values["--tag"] {
		q.Add("tag", t)
	}
	if init := last(p.values["--init"]); init != "" {
		q.Set("initiative", init)
	}
	var raw json.RawMessage
	if err := c.do(ctx, "GET", "/api/v1/attention", q, nil, &raw); err != nil {
		return nil, withNext(err, 404, "vh init ls")
	}
	return raw, nil
}

// attentionCommand is the `vh attention` line with the same filters, for a `+K more` line.
func attentionCommand(p parsedArgs) string {
	cmd := "vh attention"
	for _, t := range p.values["--tag"] {
		cmd += " --tag " + quoteArg(t)
	}
	if init := last(p.values["--init"]); init != "" {
		cmd += " --init " + quoteArg(init)
	}
	return cmd
}

// attentionText is the five sections in the order of SPEC §5.3, each a
// heading with its count and one indented line per item.
func attentionText(v attentionView, now time.Time, limit int, cmd string) string {
	return attentionSection("open asks", attentionLines(v.OpenAsks, now, askAttentionLine), limit, cmd) +
		attentionSection("done, not closed", attentionLines(v.DoneNotClosed, now, doneAttentionLine), limit, cmd) +
		attentionSection("expired leases", attentionLines(v.ExpiredLeases, now, expiredAttentionLine), limit, cmd) +
		attentionSection("overdue watches", attentionLines(v.OverdueWatches, now, watchAttentionLine), limit, cmd) +
		attentionSection("conflicts", attentionLines(v.Conflicts, now, conflictAttentionLine), limit, cmd)
}

// attentionLines renders each item with line.
func attentionLines[T any](items []T, now time.Time, line func(T, time.Time) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = line(it, now)
	}
	return out
}

// attentionSection is `<name>: <count>`, then up to limit lines (all when 0)
// and the `+K more → <command> --all` line.
func attentionSection(name string, items []string, limit int, cmd string) string {
	var b strings.Builder
	b.WriteString(name + ": " + strconv.Itoa(len(items)) + "\n")
	for i, line := range items {
		if limit > 0 && i == limit {
			b.WriteString("  +" + strconv.Itoa(len(items)-limit) + " more → " + cmd + " --all\n")
			break
		}
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// askAttentionLine is `A12  3m ago  <task|board>  <asker>  "<question>"` and
// the words `poll`, `waiting` and `blocking` that apply.
func askAttentionLine(a attentionAsk, now time.Time) string {
	task := a.Task
	if task == "" {
		task = "board"
	}
	parts := []string{a.Ref, ago(now, a.CreatedAt), task, a.Asker.String(), strconv.Quote(a.Question)}
	for _, w := range []struct {
		on   bool
		word string
	}{{a.Mode == "poll", "poll"}, {a.AskerWaiting, "waiting"}, {a.Blocking, "blocking"}} {
		if w.on {
			parts = append(parts, w.word)
		}
	}
	return strings.Join(parts, "  ")
}

// doneAttentionLine is `aa-1  <title>  done by <actor> 2m ago`, then the
// outcome of the task's last ended lease when there is one.
func doneAttentionLine(d attentionDone, now time.Time) string {
	parts := []string{d.Task, d.Title, "done by " + d.DoneBy.String() + " " + ago(now, d.DoneAt)}
	if l := d.LastLease; l != nil {
		parts = append(parts, l.Lease+" "+outcomeText(l.Outcome))
	}
	return strings.Join(parts, "  ")
}

// outcomeText is the quoted outcome of a lease, or `ended` when it had none.
func outcomeText(outcome string) string {
	if outcome == "" {
		return "ended"
	}
	return strconv.Quote(outcome)
}

// expiredAttentionLine is `L7  aa-1  <holder>  "<intent>"  renewed 5m ago`.
func expiredAttentionLine(e attentionExpired, now time.Time) string {
	return strings.Join([]string{e.Lease, e.Task, e.Holder.String(), strconv.Quote(e.Intent), "renewed " + ago(now, e.RenewedAt)}, "  ")
}

// watchAttentionLine is `aa-3  <title>  <owner>  due 3d ago`, then the last check.
func watchAttentionLine(w attentionWatch, now time.Time) string {
	parts := []string{w.Task, w.Title, w.Owner.String(), "due " + ago(now, w.NextCheckAt)}
	if c := w.LastCheck; c != nil {
		parts = append(parts, "last check "+strconv.Quote(c.Summary)+" by "+c.By.String()+" "+ago(now, c.At))
	}
	return strings.Join(parts, "  ")
}

// conflictAttentionLine is `CONFLICT <resource>: L1 <actor> <task>, L2 <actor> <task>`.
func conflictAttentionLine(c attentionConflict, _ time.Time) string {
	holders := make([]string, len(c.Holders))
	for i, h := range c.Holders {
		holders[i] = h.Lease + " " + h.Actor.String() + " " + h.Task
	}
	return "CONFLICT " + c.Resource + ": " + strings.Join(holders, ", ")
}
