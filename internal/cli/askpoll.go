package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"
)

// ExitTimeout is the exit code of `board ask --poll` when the deadline passes
// with the ask still open (SPEC §8.1).
const ExitTimeout = 3

// maxAskWait is the longest single ?wait= the server accepts, in seconds.
const maxAskWait = 300

// pollSeconds reads --poll D as whole seconds. 0 means no --poll. It is not ok
// for a bad duration and for --poll given together with --stop.
func pollSeconds(p parsedArgs) (int64, bool) {
	if !p.has("--poll") {
		return 0, true
	}
	seconds, err := parseDuration(last(p.values["--poll"]))
	return seconds, err == nil && !p.has("--stop")
}

// nextWait is the length in whole seconds of the next ?wait= call when left
// is the time to the deadline: at most maxAskWait, and left rounded UP to a
// whole second, since the server takes no shorter wait than one second. A
// remainder under a second therefore still gets a wait; the caller cuts the
// request off at the deadline itself (askFetch), so the wait never outlives
// it. There is no next call (false) once nothing is left.
func nextWait(left time.Duration) (int, bool) {
	if left <= 0 {
		return 0, false
	}
	capped := min(left, maxAskWait*time.Second)
	secs := int((capped + time.Second - 1) / time.Second)
	return secs, true
}

// askFetch reads the ask once, holding the request open up to wait seconds but
// not past left, the time to the deadline: a read cut off by the deadline returns the ask as the
// server has it then, so an answer or withdrawal made before the deadline is
// never missed.
type askFetch func(wait int, left time.Duration) (askView, json.RawMessage, error)

// pollAsk waits for the ask to leave `open` by repeating fetch until the
// deadline has elapsed. now is the clock. It returns the last ask read: still
// open when the deadline came first.
func pollAsk(first askView, raw json.RawMessage, deadline time.Time, now func() time.Time, fetch askFetch) (askView, json.RawMessage, error) {
	a := first
	for a.State == "open" {
		left := deadline.Sub(now())
		wait, ok := nextWait(left)
		if !ok {
			break
		}
		var err error
		if a, raw, err = fetch(wait, left); err != nil {
			return a, raw, err
		}
	}
	return a, raw, nil
}

// askPoll raises the ask as a poll ask, then waits for its end up to seconds.
func askPoll(p parsedArgs, body map[string]any, seconds int64, stdout, stderr io.Writer, env Env) int {
	c, err := newClient(env)
	if err != nil {
		return report(stderr, err)
	}
	ctx, stop := clientContext()
	defer stop()
	var raw json.RawMessage
	if err := c.do(ctx, "POST", "/api/v1/asks", nil, body, &raw); err != nil {
		return report(stderr, raiseFailure(p.pos[0])(err))
	}
	var first askView
	if err := json.Unmarshal(raw, &first); err != nil {
		return report(stderr, err)
	}
	deadline := nowFn().Add(time.Duration(seconds) * time.Second)
	a, raw, err := pollAsk(first, raw, deadline, nowFn, waitFetch(ctx, c, first.Ref))
	if err != nil {
		return report(stderr, askRefFailure(first.Ref)(err))
	}
	return printPollEnd(a, raw, p.has("--json"), stdout)
}

// waitFetch reads the ask with ?wait=N, cutting the request off when left, the
// time to the deadline, has run out. The request may be held longer than a plain request, so its
// timeout is the wait plus the usual one. When the deadline cuts it off, the
// ask is read once more without a wait, so the state returned is the one at
// the deadline.
func waitFetch(ctx context.Context, c *client, ref string) askFetch {
	return func(wait int, left time.Duration) (askView, json.RawMessage, error) {
		wctx, cancel := context.WithTimeout(ctx, left)
		defer cancel()
		h := *c.http
		h.Timeout = time.Duration(wait)*time.Second + requestTimeout
		long := *c
		long.http = &h
		var raw json.RawMessage
		query := url.Values{"wait": {strconv.Itoa(wait)}}
		err := long.do(wctx, "GET", askPath(ref, ""), query, nil, &raw)
		if err != nil && wctx.Err() != nil && ctx.Err() == nil {
			raw = nil
			err = c.do(ctx, "GET", askPath(ref, ""), nil, nil, &raw)
		}
		if err != nil {
			return askView{}, nil, err
		}
		var a askView
		err = json.Unmarshal(raw, &a)
		return a, raw, err
	}
}

// printPollEnd prints how the wait ended and returns the exit code: an answer
// is `ok A<n> answered by <h>: <answer>` (0), a withdrawal `A<n> withdrawn`
// with its reason (6), the deadline `A<n> still open → board ask show A<n>` (3).
// With --json the last ask is printed instead, with the same exit code.
func printPollEnd(a askView, raw json.RawMessage, asJSON bool, stdout io.Writer) int {
	code := pollExit(a.State)
	if asJSON {
		_, _ = fmt.Fprintln(stdout, string(raw))
		return code
	}
	_, _ = fmt.Fprintln(stdout, pollLine(a))
	return code
}

// pollExit is the exit code of an ask that ended the wait in state.
func pollExit(state string) int {
	switch state {
	case "answered":
		return ExitOK
	case "withdrawn":
		return ExitConflict
	}
	return ExitTimeout
}

// pollLine is the one line the end of a wait prints.
func pollLine(a askView) string {
	switch a.State {
	case "answered":
		return fmt.Sprintf("ok %s answered by %s: %s", a.Ref, a.AnsweredBy, a.Answer)
	case "withdrawn":
		if a.WithdrawReason != "" {
			return fmt.Sprintf("%s withdrawn: %s", a.Ref, a.WithdrawReason)
		}
		return a.Ref + " withdrawn"
	}
	return fmt.Sprintf("%s still open → board ask show %s", a.Ref, a.Ref)
}
