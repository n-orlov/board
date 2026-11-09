package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNextWaitNeverExceedsTheCapOrTheTimeLeft(t *testing.T) {
	for _, c := range []struct {
		left time.Duration
		want int
		ok   bool
	}{
		{20 * time.Minute, 300, true},
		{301 * time.Second, 300, true},
		{300 * time.Second, 300, true},
		{299*time.Second + 900*time.Millisecond, 300, true},
		{100 * time.Second, 100, true},
		{2500 * time.Millisecond, 3, true},
		{time.Second, 1, true},
		{999 * time.Millisecond, 1, true},
		{time.Millisecond, 1, true},
		{0, 0, false},
		{-5 * time.Second, 0, false},
	} {
		got, ok := nextWait(c.left)
		if got != c.want || ok != c.ok {
			t.Errorf("left %s: got %d,%v want %d,%v", c.left, got, ok, c.want, c.ok)
		}
	}
}

// pollClock is a clock the fetch moves: each call takes the whole wait, as a
// wait nobody answers does, but never past the time left, where the call is cut off.
type pollClock struct {
	t        time.Time
	waits    []int
	answerOn int // the call (1-based) that finds the ask answered; 0 = never
	failOn   int
}

func (c *pollClock) now() time.Time { return c.t }

func (c *pollClock) fetch(wait int, left time.Duration) (askView, json.RawMessage, error) {
	c.waits = append(c.waits, wait)
	if len(c.waits) == c.failOn {
		return askView{}, nil, errors.New("boom")
	}
	c.t = c.t.Add(min(time.Duration(wait)*time.Second, left))
	if len(c.waits) == c.answerOn {
		return askView{Ref: "A1", State: "answered"}, json.RawMessage(`{"a":1}`), nil
	}
	return askView{Ref: "A1", State: "open"}, json.RawMessage(`{"a":0}`), nil
}

func TestPollAskWaitsInSlicesEndingWithTheRemainder(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	c := &pollClock{t: start}
	a, _, err := pollAsk(askView{Ref: "A1", State: "open"}, nil, start.Add(700*time.Second), c.now, c.fetch)
	if err != nil || a.State != "open" {
		t.Fatalf("got %v %v", a, err)
	}
	if got := c.waits; len(got) != 3 || got[0] != 300 || got[1] != 300 || got[2] != 100 {
		t.Errorf("waits %v, want [300 300 100]", got)
	}
	if !c.t.Equal(start.Add(700 * time.Second)) {
		t.Errorf("the clock ended at %s", c.t)
	}
}

func TestPollAskMakesNoCallWhenTheDeadlineHasPassed(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	for _, deadline := range []time.Time{start, start.Add(-time.Hour), start.Add(-time.Millisecond)} {
		c := &pollClock{t: start}
		a, raw, err := pollAsk(askView{Ref: "A1", State: "open"}, json.RawMessage(`{"first":1}`), deadline, c.now, c.fetch)
		if err != nil || a.State != "open" || string(raw) != `{"first":1}` || len(c.waits) != 0 {
			t.Errorf("deadline %s: got %v %s %v after %v", deadline, a, raw, err, c.waits)
		}
	}
}

func TestPollAskKeepsWaitingThroughASubSecondRemainder(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		after time.Duration
		waits []int
	}{
		{900 * time.Millisecond, []int{1}},
		{time.Millisecond, []int{1}},
		{2500 * time.Millisecond, []int{3}},
		{300*time.Second + 400*time.Millisecond, []int{300, 1}},
		{600*time.Second + 999*time.Millisecond, []int{300, 300, 1}},
	} {
		deadline := start.Add(c.after)
		clock := &pollClock{t: start}
		a, _, err := pollAsk(askView{Ref: "A1", State: "open"}, nil, deadline, clock.now, clock.fetch)
		if err != nil || a.State != "open" {
			t.Fatalf("%s: got %v %v", c.after, a, err)
		}
		if fmt.Sprint(clock.waits) != fmt.Sprint(c.waits) {
			t.Errorf("%s: waits %v, want %v", c.after, clock.waits, c.waits)
		}
		if !clock.t.Equal(deadline) {
			t.Errorf("%s: gave up at %s, not at the deadline %s", c.after, clock.t, deadline)
		}
	}
}

func TestPollAskObservesAnAnswerInTheSubSecondRemainder(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	clock := &pollClock{t: start, answerOn: 2}
	a, _, err := pollAsk(askView{State: "open"}, nil, start.Add(300*time.Second+500*time.Millisecond), clock.now, clock.fetch)
	if err != nil || a.State != "answered" || len(clock.waits) != 2 || clock.waits[1] != 1 {
		t.Errorf("got %v %v after %v", a, err, clock.waits)
	}
}

func TestWaitFetchStopsAtTheDeadlineAndReadsTheAskThen(t *testing.T) {
	var waits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("wait") != "" {
			waits = append(waits, r.URL.Query().Get("wait"))
			select { // an open ask: hold the request
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		waits = append(waits, "plain")
		_, _ = w.Write([]byte(`{"ref":"A1","state":"answered","answered_by":"nik","answer":"x"}`))
	}))
	defer srv.Close()
	c := &client{base: srv.URL, http: srv.Client(), key: "k"}
	started := time.Now()
	a, _, err := waitFetch(context.Background(), c, "A1")(1, 300*time.Millisecond)
	if err != nil || a.State != "answered" {
		t.Fatalf("got %v %v", a, err)
	}
	if took := time.Since(started); took < 300*time.Millisecond || took > 2*time.Second {
		t.Errorf("returned after %s, want at the deadline", took)
	}
	if len(waits) != 2 || waits[0] != "1" || waits[1] != "plain" {
		t.Errorf("requests %v", waits)
	}
}

func TestPollAskStopsAtTheFirstClosedStateAndOnAnError(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	c := &pollClock{t: start, answerOn: 2}
	a, raw, err := pollAsk(askView{State: "open"}, nil, start.Add(time.Hour), c.now, c.fetch)
	if err != nil || a.State != "answered" || string(raw) != `{"a":1}` || len(c.waits) != 2 {
		t.Errorf("got %v %s %v after %v", a, raw, err, c.waits)
	}
	closed := &pollClock{t: start}
	if _, _, err := pollAsk(askView{State: "answered"}, nil, start.Add(time.Hour), closed.now, closed.fetch); err != nil || len(closed.waits) != 0 {
		t.Errorf("an answered ask must not be waited on: %v %v", err, closed.waits)
	}
	failing := &pollClock{t: start, failOn: 2}
	if _, _, err := pollAsk(askView{State: "open"}, nil, start.Add(time.Hour), failing.now, failing.fetch); err == nil || len(failing.waits) != 2 {
		t.Errorf("got %v after %v", err, failing.waits)
	}
}

func TestPollEndLinesAndExitCodes(t *testing.T) {
	for _, c := range []struct {
		a    askView
		line string
		code int
	}{
		{askView{Ref: "A3", State: "answered", AnsweredBy: "nik", Answer: "8080"}, "ok A3 answered by nik: 8080", ExitOK},
		{askView{Ref: "A3", State: "withdrawn", WithdrawReason: "moot"}, "A3 withdrawn: moot", ExitConflict},
		{askView{Ref: "A3", State: "withdrawn"}, "A3 withdrawn", ExitConflict},
		{askView{Ref: "A3", State: "open"}, "A3 still open → board ask show A3", ExitTimeout},
	} {
		if got := pollLine(c.a); got != c.line {
			t.Errorf("line %q, want %q", got, c.line)
		}
		var out strings.Builder
		if code := printPollEnd(c.a, json.RawMessage(`{"x":1}`), false, &out); code != c.code || out.String() != c.line+"\n" {
			t.Errorf("%s: got %d %q", c.a.State, code, out.String())
		}
		out.Reset()
		if code := printPollEnd(c.a, json.RawMessage(`{"x":1}`), true, &out); code != c.code || out.String() != "{\"x\":1}\n" {
			t.Errorf("%s json: got %d %q", c.a.State, code, out.String())
		}
	}
	if ExitTimeout != 3 {
		t.Errorf("ExitTimeout = %d", ExitTimeout)
	}
}

func TestPollSecondsReadsTheFlag(t *testing.T) {
	for _, c := range []struct {
		args []string
		secs int64
		ok   bool
	}{
		{[]string{"aa-1", "q"}, 0, true},
		{[]string{"aa-1", "q", "--poll", "10m"}, 600, true},
		{[]string{"aa-1", "q", "--poll", "90s"}, 90, true},
		{[]string{"aa-1", "q", "--poll", "10"}, 0, false},
		{[]string{"aa-1", "q", "--poll", "10m", "--stop"}, 600, false},
		{[]string{"aa-1", "q", "--stop"}, 0, true},
	} {
		p, parsed := parseArgs(c.args, askFlags)
		secs, ok := pollSeconds(p)
		if !parsed || ok != c.ok || (ok && secs != c.secs) {
			t.Errorf("%v: got %d,%v want %d,%v", c.args, secs, ok, c.secs, c.ok)
		}
	}
}

// pollBoard is a fake server: it takes the raise, then answers each GET with
// the next of states, recording the ?wait= it got.
func pollBoard(states ...string) (*fakeBoard, *[]string) {
	var waits []string
	n := 0
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		body := strings.Replace(askAnswer, `"mode":"stop"`, `"mode":"poll"`, 1)
		if r.Method == http.MethodGet {
			waits = append(waits, r.URL.Query().Get("wait"))
			body = strings.Replace(body, `"state":"open"`, `"state":"`+states[n]+`"`, 1)
			if states[n] == "answered" {
				body = strings.Replace(body, `"asker_waiting":false`, `"asker_waiting":false,"answer":"8080","answered_by":"nik"`, 1)
			}
			n++
		}
		_, _ = w.Write([]byte(body))
	}}
	return f, &waits
}

func TestAskPollRaisesAPollAskAndPrintsTheAnswer(t *testing.T) {
	f, waits := pollBoard("open", "answered")
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "ask", "aa-1", "Which port?", "--poll", "10m")
	if code != ExitOK || out != "ok A1 answered by nik: 8080\n" || errs != "" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil || body["mode"] != "poll" {
		t.Errorf("raise body %q (%v)", f.bodies[0], err)
	}
	if len(*waits) != 2 || (*waits)[0] != "300" || f.requests[1].URL.Path != "/api/v1/asks/A1" {
		t.Errorf("waits %v, path %s", *waits, f.requests[1].URL.Path)
	}
}

func TestAskPollWithdrawnExitsSixAndATimeoutExitsThree(t *testing.T) {
	f, _ := pollBoard("withdrawn")
	srv := httptest.NewServer(f)
	code, out, _ := runEnv(f.env(srv), "ask", "aa-1", "q", "--poll", "5m")
	srv.Close()
	if code != ExitConflict || out != "A1 withdrawn\n" {
		t.Errorf("withdrawn: got %d %q", code, out)
	}
	f, waits := pollBoard("open", "open", "open", "open", "open", "open")
	srv = httptest.NewServer(f)
	defer srv.Close()
	clock := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	old := nowFn
	nowFn = func() time.Time { clock = clock.Add(400 * time.Second); return clock }
	defer func() { nowFn = old }()
	code, out, _ = runEnv(f.env(srv), "ask", "aa-1", "q", "--poll", "10m")
	if code != ExitTimeout || out != "A1 still open → board ask show A1\n" {
		t.Errorf("timeout: got %d %q", code, out)
	}
	if len(*waits) != 1 || (*waits)[0] != "200" {
		t.Errorf("waits %v, want [200]", *waits)
	}
}

func TestAskPollRefusalsKeepTheirExitCodes(t *testing.T) {
	f := answering(404, `{"detail":"no such task","next":"x"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "ask", "zz-9", "q", "--poll", "5m")
	if code != ExitNotFound || out != "" || !strings.Contains(errs, "board task ls") {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}
