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
// is the time to the deadline: at most maxAskWait and never more than left, so
// the last wait is the remainder. There is no next call (false) once less than
// one whole second is left, since the server takes no shorter wait.
func nextWait(left time.Duration) (int, bool) {
	secs := int(max(0, min(left, maxAskWait*time.Second)) / time.Second)
	return secs, secs >= 1
}

// askFetch reads the ask once, holding the request open up to wait seconds.
type askFetch func(wait int) (askView, json.RawMessage, error)

// pollAsk waits for the ask to leave `open` by repeating fetch until the
// deadline. now is the clock. It returns the last ask read: still open when
// the deadline came first.
func pollAsk(first askView, raw json.RawMessage, deadline time.Time, now func() time.Time, fetch askFetch) (askView, json.RawMessage, error) {
	a := first
	for a.State == "open" {
		wait, ok := nextWait(deadline.Sub(now()))
		if !ok {
			break
		}
		var err error
		if a, raw, err = fetch(wait); err != nil {
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

// waitFetch reads the ask with ?wait=N. The request may be held longer than a
// plain request, so its timeout is the wait plus the usual one.
func waitFetch(ctx context.Context, c *client, ref string) askFetch {
	return func(wait int) (askView, json.RawMessage, error) {
		h := *c.http
		h.Timeout = time.Duration(wait)*time.Second + requestTimeout
		long := *c
		long.http = &h
		var raw json.RawMessage
		query := url.Values{"wait": {strconv.Itoa(wait)}}
		if err := long.do(ctx, "GET", askPath(ref, ""), query, nil, &raw); err != nil {
			return askView{}, nil, err
		}
		var a askView
		err := json.Unmarshal(raw, &a)
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
