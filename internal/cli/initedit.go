package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// ExitConflict is the exit code of a state conflict: the request was fine, the
// object is in a state that refuses it (SPEC §8.1).
const ExitConflict = 6

// stdin is what `--description -` reads. Tests replace it.
var stdin io.Reader = os.Stdin

// maxDescriptionBytes bounds what `--description` reads: the server refuses
// more than 20000 characters, and four bytes a character is the most UTF-8 has.
const maxDescriptionBytes = 4*20000 + 1

var withinPattern = regexp.MustCompile(`^[1-9][0-9]*d?$`)

// lsQuery is what `vh init ls` asks the server for.
type lsQuery struct {
	tags   []string
	status string
	within string
}

// lsFilter reads --tag, --status and --closed-within. A status other than
// open, closed or all, a malformed window, or a window with the open status
// (the server keeps no closed initiatives then) is not ok.
func lsFilter(p parsedArgs) (lsQuery, bool) {
	q := lsQuery{tags: p.values["--tag"], status: last(p.values["--status"]), within: last(p.values["--closed-within"])}
	if q.status != "" && q.status != "open" && q.status != "closed" && q.status != "all" {
		return q, false
	}
	if q.within == "" {
		return q, true
	}
	return q, withinPattern.MatchString(q.within) && (q.status == "closed" || q.status == "all")
}

func last(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

func (q lsQuery) values() url.Values {
	v := url.Values{"tag": q.tags}
	if q.status != "" {
		v.Set("status", q.status)
	}
	if q.within != "" {
		v.Set("closed_within", q.within)
	}
	return v
}

// command is the `vh init ls` line that shows the same initiatives, with
// --all, for the `+K more` line.
func (q lsQuery) command() string {
	cmd := "vh init ls"
	for _, t := range q.tags {
		cmd += " --tag " + t
	}
	if q.status != "" {
		cmd += " --status " + q.status
	}
	if q.within != "" {
		cmd += " --closed-within " + q.within
	}
	return cmd
}

// editBody builds the PATCH body from the flags. It is not ok when no flag
// changes anything or two flags contradict (--tag with --no-tags, a date with
// the flag that clears it).
func editBody(p parsedArgs, description *string) (map[string]any, bool) {
	body := map[string]any{}
	if v := p.values["--title"]; len(v) > 0 {
		body["title"] = last(v)
	}
	if description != nil {
		body["description"] = *description
	}
	if !editTags(p, body) || !editDate(p, body, "--start", "--clear-start", "target_start") || !editDate(p, body, "--end", "--clear-end", "target_end") {
		return nil, false
	}
	return body, len(body) > 0
}

func editTags(p parsedArgs, body map[string]any) bool {
	tags := p.values["--tag"]
	if p.has("--no-tags") {
		body["tags"] = []string{}
		return len(tags) == 0
	}
	if len(tags) > 0 {
		body["tags"] = tags
	}
	return true
}

// editDate sets field from the flag, or to null from the clearing flag; both is not ok.
func editDate(p parsedArgs, body map[string]any, set, unset, field string) bool {
	if v := p.values[set]; len(v) > 0 {
		body[field] = last(v)
		return !p.has(unset)
	}
	if p.has(unset) {
		body[field] = nil
	}
	return true
}

// readDescription reads the text of --description: standard input for "-", else the named file.
func readDescription(src string) (string, error) {
	return readTextSource(src, "description")
}

// readTextSource reads a text flag's value: standard input for "-", else the
// named file. what names the flag's subject in an error.
func readTextSource(src, what string) (string, error) {
	r := stdin
	if src != "-" {
		f, err := os.Open(src) //nolint:gosec // G304: the operator names the text file on the command line
		if err != nil {
			return "", fmt.Errorf("cannot read the %s file %s", what, src)
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	raw, err := io.ReadAll(io.LimitReader(r, maxDescriptionBytes))
	if err != nil {
		return "", fmt.Errorf("cannot read the %s from %s: %w", what, src, err)
	}
	return string(raw), nil
}

func initEdit(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, initEditFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, InitUsage)
	}
	var description *string
	if src := last(p.values["--description"]); src != "" {
		text, err := readDescription(src)
		if err != nil {
			return report(stderr, err)
		}
		description = &text
	}
	body, ok := editBody(p, description)
	if !ok {
		return usageLine(stderr, InitUsage)
	}
	return patchInitiative(p, "PATCH", "", body, "updated", stdout, stderr, env)
}

func initClose(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, initCloseFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, InitUsage)
	}
	body := map[string]any{}
	if p.has("--force") {
		body["force"] = true
	}
	return patchInitiative(p, "POST", "/close", body, "closed", stdout, stderr, env)
}

func initReopen(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, initReopenFlags)
	if !ok || len(p.pos) != 1 {
		return usageLine(stderr, InitUsage)
	}
	return patchInitiative(p, "POST", "/reopen", nil, "reopened", stdout, stderr, env)
}

// patchInitiative sends one write to the initiative p names and prints the
// `ok` line (or the object with --json). An unknown slug exits 4 and a state
// conflict exits 6, each with the command that follows.
func patchInitiative(p parsedArgs, method, suffix string, body any, verb string, stdout, stderr io.Writer, env Env) int {
	slug := p.pos[0]
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, method, "/api/v1/initiatives/"+url.PathEscape(slug)+suffix, nil, body, &raw); err != nil {
		return report(stderr, writeFailure(err, slug, suffix))
	}
	if p.has("--json") {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return ExitOK
	}
	var in initiative
	if err := json.Unmarshal(raw, &in); err != nil {
		return report(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "ok initiative %s %s\n", in.Slug, verb)
	return ExitOK
}

// writeFailure words the next action of a refused edit, close or reopen: an
// unknown slug exits 4 and a 409 exits 6.
func writeFailure(err error, slug, suffix string) error {
	err = withNext(err, http.StatusNotFound, "vh init ls")
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		return err
	}
	return &apiError{Status: ae.Status, Detail: ae.Detail, Next: conflictNext(ae, slug, suffix), Exit: ExitConflict}
}

// conflictNext is the command that follows a 409. The server names the force
// option when tasks stop the close; any other close conflict means the
// initiative is closed already.
func conflictNext(ae *apiError, slug, suffix string) string {
	switch {
	case suffix == "/close" && strings.Contains(ae.Next, "force"):
		return "vh init close " + slug + " --force"
	case suffix == "/close":
		return "vh init reopen " + slug
	default:
		return "vh init show " + slug
	}
}
