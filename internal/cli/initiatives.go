package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// InitUsage is the single stderr line for a malformed `vh init` command line.
const InitUsage = "usage: vh init add <slug> \"<title>\" [--tag t]... | ls [--tag t]... [--status open|closed|all] [--closed-within N] [--limit N|--all] | show <slug> | edit <slug> [--title T] [--description file|-] [--tag t]... [--no-tags] [--start D] [--end D] [--clear-start] [--clear-end] | close <slug> [--force] | reopen <slug>"

// WhoamiUsage is the single stderr line for a malformed `vh whoami`.
const WhoamiUsage = "usage: vh whoami"

// DefaultListCap is how many items a plain list command shows (SPEC §8.1).
const DefaultListCap = 20

// listPageSize is the page size the CLI asks the server for.
const listPageSize = 1000

var nowFn = time.Now

// initiative is the part of an initiative the text output shows.
type initiative struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Tags        []string `json:"tags"`
	TargetStart string   `json:"target_start"`
	TargetEnd   string   `json:"target_end"`
	CreatedBy   string   `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
	ClosedAt    string   `json:"closed_at"`
	ClosedBy    string   `json:"closed_by"`
	Attention   int      `json:"attention_count"`
}

type initiativePage struct {
	Items     []json.RawMessage `json:"items"`
	NextAfter string            `json:"next_after"`
}

// clientContext is the context of a client command: cancelled on SIGINT/SIGTERM.
func clientContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// initCmd runs `vh init ...` against the server named by VH_URL.
func initCmd(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		return usageLine(stderr, InitUsage)
	}
	switch args[0] {
	case "add":
		return initAdd(args[1:], stdout, stderr, env)
	case "ls":
		return initLs(args[1:], stdout, stderr, env)
	case "show":
		return initShow(args[1:], stdout, stderr, env)
	case "edit":
		return initEdit(args[1:], stdout, stderr, env)
	case "close":
		return initClose(args[1:], stdout, stderr, env)
	case "reopen":
		return initReopen(args[1:], stdout, stderr, env)
	default:
		return usageLine(stderr, InitUsage)
	}
}

func usageLine(stderr io.Writer, line string) int {
	_, _ = fmt.Fprintln(stderr, line)
	return ExitUsage
}

func initAdd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, initAddFlags)
	if !ok || len(p.pos) != 2 {
		return usageLine(stderr, InitUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	body := map[string]any{"slug": p.pos[0], "title": p.pos[1]}
	if tags := p.values["--tag"]; len(tags) > 0 {
		body["tags"] = tags
	}
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/initiatives", nil, body, &raw); err != nil {
		return report(stderr, withNext(err, http.StatusConflict, "vh init show "+p.pos[0]))
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var in initiative
	if err := json.Unmarshal(raw, &in); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "ok initiative %s created\n", in.Slug)
	return ExitOK
}

// listCap reads --limit and --all: the number of items to show, 0 for all.
func listCap(p parsedArgs) (int, bool) {
	limits := p.values["--limit"]
	if p.has("--all") {
		return 0, len(limits) == 0
	}
	if len(limits) == 0 {
		return DefaultListCap, true
	}
	n, err := strconv.Atoi(limits[len(limits)-1])
	return n, err == nil && n >= 1
}

func initLs(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, initLsFlags)
	limit, okCap := listCap(p)
	filter, okFilter := lsFilter(p)
	if !ok || !okCap || !okFilter || len(p.pos) != 0 {
		return usageLine(stderr, InitUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	items, err := c.allInitiatives(ctx, filter)
	if err != nil {
		return report(stderr, err)
	}
	items, more := capItems(items, limit)
	if p.has("--json") {
		return printJSON(stdout, stderr, items)
	}
	return printInitiativeLines(stdout, stderr, items, more, filter.command())
}

// capItems keeps the first limit items (all when limit is 0) and counts the rest.
func capItems(items []json.RawMessage, limit int) ([]json.RawMessage, int) {
	if limit > 0 && len(items) > limit {
		return items[:limit], len(items) - limit
	}
	return items, 0
}

// allInitiatives follows next_after until the last page.
func (c *client) allInitiatives(ctx context.Context, f lsQuery) ([]json.RawMessage, error) {
	items := []json.RawMessage{}
	after := ""
	for {
		q := f.values()
		q.Set("limit", strconv.Itoa(listPageSize))
		if after != "" {
			q.Set("after", after)
		}
		var page initiativePage
		if err := c.do(ctx, "GET", "/api/v1/initiatives", q, nil, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.NextAfter == "" || page.NextAfter == after {
			return items, nil
		}
		after = page.NextAfter
	}
}

func printJSON(stdout, stderr io.Writer, v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprintln(stdout, string(raw))
	return ExitOK
}

func printInitiativeLines(stdout, stderr io.Writer, items []json.RawMessage, more int, cmd string) int {
	for _, raw := range items {
		var in initiative
		if err := json.Unmarshal(raw, &in); err != nil {
			return report(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, initiativeLine(in))
	}
	if more > 0 {
		_, _ = fmt.Fprintf(stdout, "+%d more → %s --all\n", more, cmd)
	}
	return ExitOK
}

// initiativeLine is the one-line form: slug, status, title, the tags if any and ` !N` when N tasks need attention.
func initiativeLine(in initiative) string {
	line := in.Slug + "  " + in.Status + "  " + in.Title
	if len(in.Tags) > 0 {
		line += "  [" + strings.Join(in.Tags, ",") + "]"
	}
	if in.Attention > 0 {
		line += " !" + strconv.Itoa(in.Attention)
	}
	return line
}

func initShow(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, initShowFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, InitUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "GET", "/api/v1/initiatives/"+url.PathEscape(p.pos[0]), nil, nil, &raw); err != nil {
		return report(stderr, withNext(err, http.StatusNotFound, "vh init ls"))
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var in initiative
	if err := json.Unmarshal(raw, &in); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprint(stdout, initiativeDetail(in, nowFn()))
	return ExitOK
}

// initiativeDetail is the `init show` text: the one-line form, then a line per
// fact the initiative has, then the description.
func initiativeDetail(in initiative, now time.Time) string {
	var b strings.Builder
	b.WriteString(initiativeLine(in) + "\n")
	if in.TargetStart != "" || in.TargetEnd != "" {
		b.WriteString("targets: " + in.TargetStart + " → " + in.TargetEnd + "\n")
	}
	b.WriteString("created by " + in.CreatedBy + " " + ago(now, in.CreatedAt) + "\n")
	if in.ClosedAt != "" {
		b.WriteString("closed by " + in.ClosedBy + " " + ago(now, in.ClosedAt) + "\n")
	}
	if in.Description != "" {
		b.WriteString(in.Description + "\n")
	}
	return b.String()
}

// ago renders an RFC 3339 time relative to now: 5m ago, 2h ago, 3d ago.
func ago(now time.Time, stamp string) string {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}
