package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// NoteUsage is the single stderr line for a malformed `board note` command line.
const NoteUsage = "usage: board note \"<summary>\" [--body file|-] [--task ref] [--init slug] [--res r] [--mention h]... [--json] | show <N…> [--json]"

// NotesUsage is the single stderr line for a malformed `board notes` command line.
const NotesUsage = "usage: board notes [--since last|<time>|<N…>] [--task ref] [--init slug] [--broadcast] [--res r]... [--mention h] [--ack] [--limit N|--all] [--json]"

// DefaultNotesCap is how many notes `board notes` shows unless --limit or --all says otherwise (SPEC §8.1).
const DefaultNotesCap = 5

// noteView is the part of a note the text output shows.
type noteView struct {
	Ref        string   `json:"ref"`
	Author     string   `json:"author"`
	Summary    string   `json:"summary"`
	Body       string   `json:"body"`
	HasBody    bool     `json:"has_body"`
	Task       string   `json:"task"`
	Initiative string   `json:"initiative"`
	Resource   string   `json:"resource"`
	Mentions   []string `json:"mentions"`
	Broadcast  bool     `json:"broadcast"`
	CreatedAt  string   `json:"created_at"`
}

// noteCmd runs `board note ...`: a first word of `show` is the subcommand, any
// other word starts a new note.
func noteCmd(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) > 0 && args[0] == "show" {
		return noteShow(args[1:], stdout, stderr, env)
	}
	return notePost(args, stdout, stderr, env)
}

// notePost runs `board note "<summary>"` and prints `ok note N<n> posted`.
func notePost(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, noteFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, NoteUsage)
	}
	body, err := noteBody(p)
	if err != nil {
		return report(stderr, err)
	}
	raw, err := sendNote(body, env)
	if err != nil {
		return report(stderr, err)
	}
	return printNote(p, raw, func(n noteView) string { return "ok note " + n.Ref + " posted\n" }, stdout, stderr)
}

// sendNote posts the note and returns the answer.
func sendNote(body map[string]any, env Env) (json.RawMessage, error) {
	c, err := newClient(env)
	if err != nil {
		return nil, err
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/notes", nil, body, &raw); err != nil {
		return nil, eventsFailure(err)
	}
	return raw, nil
}

// printNote prints the note raw as render makes it, or as it is with --json.
func printNote(p parsedArgs, raw json.RawMessage, render func(noteView) string, stdout, stderr io.Writer) int {
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var n noteView
	if err := json.Unmarshal(raw, &n); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprint(stdout, render(n))
	return ExitOK
}

// noteBody builds the POST body of a new note from the summary and the flags.
func noteBody(p parsedArgs) (map[string]any, error) {
	body := map[string]any{"summary": p.pos[0]}
	if src := last(p.values["--body"]); src != "" {
		text, err := readTextSource(src, "body")
		if err != nil {
			return nil, err
		}
		body["body"] = text
	}
	for field, flag := range map[string]string{"task": "--task", "initiative": "--init", "resource": "--res"} {
		if v := last(p.values[flag]); v != "" {
			body[field] = v
		}
	}
	if m := p.values["--mention"]; len(m) > 0 {
		body["mentions"] = m
	}
	return body, nil
}

// noteShow runs `board note show <N…>`.
func noteShow(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, noteShowFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, NoteUsage)
	}
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "GET", "/api/v1/notes/"+url.PathEscape(p.pos[0]), nil, nil, &raw); err != nil {
		return report(stderr, withNext(err, http.StatusNotFound, "board notes"))
	}
	return printNote(p, raw, func(n noteView) string { return noteDetail(n, nowFn()) }, stdout, stderr)
}

// noteLine is `N3  3m ago  <author>  <task|initiative|broadcast>  [<resource>]  <summary>`,
// ending `  +` when the note has a body.
func noteLine(n noteView, now time.Time) string {
	parts := []string{n.Ref, ago(now, n.CreatedAt), n.Author, noteScope(n)}
	if n.Resource != "" {
		parts = append(parts, "["+n.Resource+"]")
	}
	parts = append(parts, n.Summary)
	if n.HasBody {
		parts = append(parts, "+")
	}
	return strings.Join(parts, "  ")
}

// noteScope says what the note is on: its task, else its initiative, else the whole board.
func noteScope(n noteView) string {
	switch {
	case n.Task != "":
		return n.Task
	case n.Initiative != "":
		return n.Initiative
	}
	return "broadcast"
}

// noteDetail is the `note show` text: the one-line form, `initiative:` and
// `mentions:` lines when they apply, then the body.
func noteDetail(n noteView, now time.Time) string {
	var b strings.Builder
	b.WriteString(noteLine(n, now) + "\n")
	if n.Task != "" && n.Initiative != "" {
		b.WriteString("initiative: " + n.Initiative + "\n")
	}
	if len(n.Mentions) > 0 {
		b.WriteString("mentions: " + strings.Join(n.Mentions, ", ") + "\n")
	}
	if n.Body != "" {
		b.WriteString(n.Body)
		if !strings.HasSuffix(n.Body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// noteQuery is what `board notes` asks the server for.
type noteQuery struct {
	since, task, initiative, mention string
	broadcast, ack                   bool
	resources                        []string
}

var noteRef = regexp.MustCompile(`^[nN]\d+$`)

// noteSince turns the value of --since into the `since` query: `last` is the
// cursor, an N<n> ref is upper-cased, a time or a date is sent as RFC 3339 and
// anything else (a note id) goes as it is.
func noteSince(s string) string {
	switch {
	case s == "last":
		return "cursor"
	case noteRef.MatchString(s):
		return strings.ToUpper(s)
	}
	if t, err := parseTime(s); err == nil {
		return t
	}
	return s
}

// notesFilter reads the filters of `board notes`.
func notesFilter(p parsedArgs) noteQuery {
	return noteQuery{
		since: noteSince(last(p.values["--since"])), task: last(p.values["--task"]),
		initiative: last(p.values["--init"]), mention: last(p.values["--mention"]),
		broadcast: p.has("--broadcast"), ack: p.has("--ack"), resources: p.values["--res"],
	}
}

func (q noteQuery) values() url.Values {
	v := url.Values{}
	for k, val := range map[string]string{"since": q.since, "task": q.task, "initiative": q.initiative, "mention": q.mention} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if q.broadcast {
		v.Set("broadcast", "only")
	}
	for _, r := range q.resources {
		v.Add("resource", r)
	}
	return v
}

// command is the `board notes` line that shows the same notes, with --all.
func (q noteQuery) command() string {
	cmd := "board notes"
	since := q.since
	if since == "cursor" {
		since = "last"
	}
	for _, f := range []struct{ flag, val string }{{"since", since}, {"task", q.task}, {"init", q.initiative}, {"mention", q.mention}} {
		if f.val != "" {
			cmd += " --" + f.flag + " " + quoteArg(f.val)
		}
	}
	if q.broadcast {
		cmd += " --broadcast"
	}
	for _, r := range q.resources {
		cmd += " --res " + quoteArg(r)
	}
	if q.ack {
		cmd += " --ack"
	}
	return cmd
}

// notesCap reads --limit and --all like the other lists, with a cap of 5 by default.
func notesCap(p parsedArgs) (int, bool) {
	if len(p.values["--limit"]) == 0 && !p.has("--all") {
		return DefaultNotesCap, true
	}
	return listCap(p)
}

// notesCmd runs `board notes`.
func notesCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, notesFlags)
	limit, okCap := notesCap(p)
	if !ok || !okCap || len(p.pos) != 0 {
		return usageLine(stderr, NotesUsage)
	}
	q := notesFilter(p)
	items, more, err := readNotes(q, limit, env)
	if err != nil {
		return report(stderr, err)
	}
	if p.has("--json") {
		return printJSON(stdout, stderr, items)
	}
	return printNoteLines(stdout, stderr, items, more, q.command())
}

// readNotes reads the notes q selects and keeps the first limit (all when 0).
// With --ack the caller's cursor then moves to the last note kept.
func readNotes(q noteQuery, limit int, env Env) ([]json.RawMessage, int, error) {
	c, err := newClient(env)
	if err != nil {
		return nil, 0, err
	}
	ctx, stop := clientContext()
	defer stop()
	items, err := c.allPages(ctx, "/api/v1/notes", q.values())
	if err != nil {
		return nil, 0, eventsFailure(err)
	}
	items, more := capItems(items, limit)
	if q.ack && len(items) > 0 {
		err = c.ackNotes(ctx, items[len(items)-1])
	}
	return items, more, err
}

// ackNotes moves the caller's notes cursor to the note last, the last one shown:
// notes that were left out by the cap stay unread.
func (c *client) ackNotes(ctx context.Context, lastShown json.RawMessage) error {
	var n noteView
	if err := json.Unmarshal(lastShown, &n); err != nil {
		return err
	}
	return c.do(ctx, "PUT", "/api/v1/cursors/notes", nil, map[string]string{"position": n.Ref}, nil)
}

func printNoteLines(stdout, stderr io.Writer, items []json.RawMessage, more int, cmd string) int {
	now := nowFn()
	for _, raw := range items {
		var n noteView
		if err := json.Unmarshal(raw, &n); err != nil {
			return report(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, noteLine(n, now))
	}
	if more > 0 {
		_, _ = fmt.Fprintf(stdout, "+%d more → %s --all\n", more, cmd)
	}
	return ExitOK
}
