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
		{600*time.Second + 999*time.Millisecond, 300, true},
		{450 * time.Second, 150, true}, // ends a whole cap before the deadline
		{301500 * time.Millisecond, 2, true},
		{301 * time.Second, 1, true},
		{300*time.Second + time.Millisecond, 1, true},
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
// wait nobody answers does, but never past until, where the server ends it.
type pollClock struct {
	t        time.Time
	waits    []int
	untils   []time.Time
	answerOn int // the call (1-based) that finds the ask answered; 0 = never
	failOn   int
}

func (c *pollClock) now() time.Time { return c.t }

func (c *pollClock) fetch(wait int, until time.Time) read {
	c.waits, c.untils = append(c.waits, wait), append(c.untils, until)
	if len(c.waits) == c.failOn {
		return read{err: errors.New("boom")}
	}
	c.t = c.t.Add(min(time.Duration(wait)*time.Second, until.Sub(c.t)))
	if len(c.waits) == c.answerOn {
		return read{a: askView{Ref: "A1", State: "answered"}, raw: json.RawMessage(`{"a":1}`)}
	}
	return read{a: askView{Ref: "A1", State: "open"}, raw: json.RawMessage(`{"a":0}`)}
}

func openRead() read { return read{a: askView{Ref: "A1", State: "open"}} }

// A poll longer than the server's cap waits in capped slices, the first ones
// ending a whole cap before the deadline, so the last call is the one that runs
// to the deadline; every call carries the deadline as until.
func TestPollAskWaitsInSlicesWhoseLastReachesTheDeadline(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(700 * time.Second)
	c := &pollClock{t: start}
	r := pollAsk(openRead(), deadline, c.now, c.fetch)
	if r.err != nil || r.a.State != "open" {
		t.Fatalf("got %v %v", r.a, r.err)
	}
	if got := c.waits; fmt.Sprint(got) != "[300 100 300]" {
		t.Errorf("waits %v, want [300 100 300]", got)
	}
	for _, u := range c.untils {
		if !u.Equal(deadline) {
			t.Errorf("until %s, want the deadline %s", u, deadline)
		}
	}
	if !c.t.Equal(deadline) {
		t.Errorf("the clock ended at %s", c.t)
	}
}

func TestPollAskMakesNoCallWhenTheDeadlineHasPassed(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	for _, deadline := range []time.Time{start, start.Add(-time.Hour), start.Add(-time.Millisecond)} {
		c := &pollClock{t: start}
		r := pollAsk(read{a: askView{Ref: "A1", State: "open"}, raw: json.RawMessage(`{"first":1}`)}, deadline, c.now, c.fetch)
		if r.err != nil || r.a.State != "open" || string(r.raw) != `{"first":1}` || len(c.waits) != 0 {
			t.Errorf("deadline %s: got %v %s %v after %v", deadline, r.a, r.raw, r.err, c.waits)
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
		{300*time.Second + 400*time.Millisecond, []int{1, 300}},
		{600*time.Second + 999*time.Millisecond, []int{300, 1, 300}},
	} {
		deadline := start.Add(c.after)
		clock := &pollClock{t: start}
		r := pollAsk(openRead(), deadline, clock.now, clock.fetch)
		if r.err != nil || r.a.State != "open" {
			t.Fatalf("%s: got %v %v", c.after, r.a, r.err)
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
	r := pollAsk(openRead(), start.Add(300*time.Second+500*time.Millisecond), clock.now, clock.fetch)
	if r.err != nil || r.a.State != "answered" || fmt.Sprint(clock.waits) != "[1 300]" {
		t.Errorf("got %v %v after %v", r.a, r.err, clock.waits)
	}
}

// endedView is the ask in state, answered or withdrawn, with stamp.
func endedView(state, stamp string) askView {
	if state == "withdrawn" {
		return askView{Ref: "A1", State: state, WithdrawnAt: stamp}
	}
	return askView{Ref: "A1", State: state, AnsweredAt: stamp}
}

// lateFetch returns, on its first call, the ask as ended at the given offset
// from start.
func lateFetch(start time.Time, state string, endedAt time.Duration) askFetch {
	return func(_ int, _ time.Time) read {
		return read{a: endedView(state, start.Add(endedAt).Format(time.RFC3339Nano)), raw: json.RawMessage(`{"read":"after"}`)}
	}
}

func TestPollAskOnlyCountsAnEndEffectiveByTheDeadline(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(10 * time.Second)
	for _, c := range []struct {
		state string
		at    time.Duration
		want  string
		code  int
	}{
		{"answered", 9999 * time.Millisecond, "answered", ExitOK},
		{"answered", 10*time.Second - time.Millisecond, "answered", ExitOK},
		{"answered", 10 * time.Second, "open", ExitTimeout}, // stamped in the deadline's own millisecond: it may be after it
		{"answered", 10*time.Second + time.Millisecond, "open", ExitTimeout},
		{"answered", 11 * time.Second, "open", ExitTimeout},
		{"withdrawn", 9 * time.Second, "withdrawn", ExitConflict},
		{"withdrawn", 10*time.Second - time.Millisecond, "withdrawn", ExitConflict},
		{"withdrawn", 10 * time.Second, "open", ExitTimeout},
		{"withdrawn", 10*time.Second + time.Millisecond, "open", ExitTimeout},
		{"withdrawn", time.Hour, "open", ExitTimeout},
	} {
		clock := &pollClock{t: start}
		r := pollAsk(openRead(), deadline, clock.now, lateFetch(start, c.state, c.at))
		if r.err != nil || r.a.State != c.want || pollExit(r.a.State) != c.code {
			t.Errorf("%s at %s: got %q exit %d (%v), want %q exit %d", c.state, c.at, r.a.State, pollExit(r.a.State), r.err, c.want, c.code)
		}
	}
}

// A deadline with a sub-millisecond part shares its millisecond with stamps of
// ends made both before and after it, since the server floors a stamp to the
// millisecond: only a stamp whose whole millisecond is over by the deadline
// counts, and a stamp carrying its own sub-millisecond part is taken as exact,
// whenever the server's answer reaches the client.
func TestPollAskCountsAMillisecondStampOnlyWhenItsMillisecondIsOverByTheDeadline(t *testing.T) {
	start := time.Date(2026, 11, 9, 20, 30, 0, 0, time.UTC)
	deadline := start.Add(time.Second + 500*time.Microsecond) // 20:30:01.0005
	for _, c := range []struct {
		state string
		stamp string
		code  int
	}{
		{"answered", "2026-11-09T20:30:00.999Z", ExitOK},
		{"withdrawn", "2026-11-09T20:30:00.999Z", ExitConflict},
		{"answered", "2026-11-09T20:30:01Z", ExitTimeout}, // made at .0006 or at .0004: not provably in time
		{"withdrawn", "2026-11-09T20:30:01.000Z", ExitTimeout},
		{"answered", "2026-11-09T20:30:01.002Z", ExitTimeout},
		{"withdrawn", "2026-11-09T20:30:01.0004Z", ExitConflict},
		{"answered", "2026-11-09T20:30:01.0005Z", ExitOK},
		{"answered", "2026-11-09T20:30:01.0006Z", ExitTimeout},
	} {
		for _, readAt := range []time.Time{deadline, deadline.Add(time.Microsecond), deadline.Add(time.Minute)} {
			clock := &pollClock{t: start}
			fetch := func(_ int, _ time.Time) read {
				clock.t = readAt // the server's answer reaches the client then
				return read{a: endedView(c.state, c.stamp)}
			}
			got := pollAsk(openRead(), deadline, clock.now, fetch)
			if got.err != nil || pollExit(got.a.State) != c.code {
				t.Errorf("%s at %s back at %s: got %q exit %d (%v), want exit %d", c.state, c.stamp,
					readAt.Format(time.RFC3339Nano), got.a.State, pollExit(got.a.State), got.err, c.code)
			}
		}
	}
}

// The server's answer to the call that runs to the deadline is the ask as it
// stood then: an end it reports is counted, by its own stamp, even when the
// answer reaches the client well after the deadline, and an ask it reports open
// is exit 3 however late it comes back. Nothing is read again.
func TestPollAskCountsTheServersAnswerHoweverLateItArrives(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(10 * time.Second)
	for _, c := range []struct {
		state  string
		stamp  time.Duration // from start
		backAt time.Duration // when the answer reaches the client, from start
		code   int
	}{
		{"answered", 9 * time.Second, 10 * time.Second, ExitOK},
		{"withdrawn", 9 * time.Second, 10 * time.Second, ExitConflict},
		{"answered", 9 * time.Second, 10*time.Second + time.Nanosecond, ExitOK},
		{"withdrawn", 9 * time.Second, 10*time.Second + time.Nanosecond, ExitConflict},
		{"answered", time.Second, time.Minute, ExitOK},
		{"withdrawn", 5 * time.Second, 10*time.Second + 75*time.Millisecond, ExitConflict},
		{"open", 0, 10*time.Second + 75*time.Millisecond, ExitTimeout},
		{"open", 0, time.Minute, ExitTimeout},
	} {
		a := endedView(c.state, start.Add(c.stamp).Format(time.RFC3339Nano))
		clock := &pollClock{t: start}
		calls := 0
		fetch := func(_ int, _ time.Time) read {
			calls++
			clock.t = start.Add(c.backAt)
			return read{a: a, raw: json.RawMessage(`{"read":1}`)}
		}
		got := pollAsk(openRead(), deadline, clock.now, fetch)
		if got.err != nil || pollExit(got.a.State) != c.code || calls != 1 || string(got.raw) != `{"read":1}` {
			t.Errorf("%s stamped %s back at %s: got %q exit %d raw %s after %d calls (%v), want exit %d",
				c.state, c.stamp, c.backAt, got.a.State, pollExit(got.a.State), got.raw, calls, got.err, c.code)
		}
	}
}

func TestPollDeadlineRoundsUpToAWholeMillisecond(t *testing.T) {
	at := func(ns int) time.Time { return time.Date(2026, 3, 4, 12, 0, 0, ns, time.UTC) }
	for _, c := range []struct {
		now     time.Time
		seconds int64
		want    time.Time
	}{
		{at(0), 600, at(0).Add(600 * time.Second)},
		{at(400_000), 600, at(1_000_000).Add(600 * time.Second)},
		{at(1), 1, at(1_000_000).Add(time.Second)},
		{at(999_999_999), 0, at(999_999_999).Truncate(time.Second).Add(time.Second)},
		{at(7_000_000), 30, at(7_000_000).Add(30 * time.Second)},
	} {
		if got := pollDeadline(c.now, c.seconds); !got.Equal(c.want) {
			t.Errorf("pollDeadline(%s, %d) = %s, want %s", c.now.Format(time.RFC3339Nano), c.seconds, got.Format(time.RFC3339Nano), c.want.Format(time.RFC3339Nano))
		}
		if got := pollDeadline(c.now, c.seconds); got.Before(c.now.Add(time.Duration(c.seconds) * time.Second)) {
			t.Errorf("pollDeadline(%s, %d) = %s is before the requested deadline", c.now, c.seconds, got)
		}
	}
}

// Raised at 12:00:00.0004 with --poll 10m, the deadline is 12:10:00.001: an end
// the server stamps 12:10:00.000 was made before it, even when after
// 12:10:00.0004, and one stamped 12:10:00.001 was not.
func TestAskPollRoundsTheDeadlineUpToTheServersMillisecond(t *testing.T) {
	for _, c := range []struct {
		state, field, stamp, out string
		code                     int
	}{
		{"answered", "answered_at", "2026-03-04T12:10:00.000Z", "ok A1 answered by nik: x\n", ExitOK},
		{"withdrawn", "withdrawn_at", "2026-03-04T12:10:00.000Z", "A1 withdrawn\n", ExitConflict},
		{"answered", "answered_at", "2026-03-04T12:10:00.001Z", "A1 still open → board ask show A1\n", ExitTimeout},
		{"withdrawn", "withdrawn_at", "2026-03-04T12:10:00.001Z", "A1 still open → board ask show A1\n", ExitTimeout},
	} {
		f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
			body := strings.Replace(askAnswer, `"mode":"stop"`, `"mode":"poll"`, 1)
			if r.Method == http.MethodGet {
				body = strings.Replace(body, `"state":"open"`, `"state":"`+c.state+`","`+c.field+`":"`+c.stamp+`","answer":"x","answered_by":"nik"`, 1)
			}
			_, _ = w.Write([]byte(body))
		}}
		srv := httptest.NewServer(f)
		old := nowFn
		clock := time.Date(2026, 3, 4, 12, 0, 0, 400_000, time.UTC)
		calls := 0
		nowFn = func() time.Time {
			calls++
			if calls == 1 {
				return clock
			}
			return clock.Add(10*time.Minute - time.Second) // one second before the requested deadline
		}
		code, out, _ := runEnv(f.env(srv), "ask", "aa-1", "q", "--poll", "10m")
		nowFn = old
		srv.Close()
		if code != c.code || out != c.out {
			t.Errorf("%s stamped %s: got %d %q, want %d %q", c.state, c.stamp, code, out, c.code, c.out)
		}
	}
}

func TestPollAskLateEndStopsPollingAndSurvivesSubSecondRemainder(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	calls := 0
	fetch := func(_ int, _ time.Time) read {
		calls++
		return read{a: endedView("answered", start.Add(901*time.Millisecond).Format(time.RFC3339Nano))}
	}
	clock := &pollClock{t: start}
	r := pollAsk(openRead(), start.Add(900*time.Millisecond), clock.now, fetch)
	if r.err != nil || r.a.State != "open" || calls != 1 {
		t.Errorf("got %v after %d calls (%v)", r.a, calls, r.err)
	}
}

func TestAskPollAnAnswerAfterTheDeadlineExitsThree(t *testing.T) {
	for _, c := range []struct{ state, field string }{{"answered", "answered_at"}, {"withdrawn", "withdrawn_at"}} {
		f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
			body := strings.Replace(askAnswer, `"mode":"stop"`, `"mode":"poll"`, 1)
			if r.Method == http.MethodGet {
				body = strings.Replace(body, `"state":"open"`, `"state":"`+c.state+`","`+c.field+`":"2026-03-04T12:10:00.001Z","answer":"x","answered_by":"nik"`, 1)
			}
			_, _ = w.Write([]byte(body))
		}}
		srv := httptest.NewServer(f)
		old := nowFn
		clock := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
		nowFn = func() time.Time { t := clock; clock = clock.Add(10 * time.Minute); return t } // the deadline is 12:10:00
		code, out, _ := runEnv(f.env(srv), "ask", "aa-1", "q", "--poll", "10m")
		nowFn = old
		srv.Close()
		if code != ExitTimeout || out != "A1 still open → board ask show A1\n" {
			t.Errorf("%s after the deadline: got %d %q", c.state, code, out)
		}
	}
}

// getAsk sends the wait and the deadline as until, and is not cut off at the
// deadline: an answer the server holds back past it still arrives and counts.
func TestGetAskSendsUntilAndTakesAnAnswerThatArrivesAfterIt(t *testing.T) {
	until := time.Now().Add(200 * time.Millisecond).Truncate(time.Millisecond)
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		time.Sleep(time.Until(until.Add(400 * time.Millisecond))) // the answer as of until, delayed
		_, _ = w.Write([]byte(`{"ref":"A1","state":"withdrawn"}`))
	}))
	defer srv.Close()
	c := &client{base: srv.URL, http: srv.Client(), key: "k"}
	r := waitFetch(context.Background(), c, "A1")(1, until)
	if r.err != nil || r.a.State != "withdrawn" || string(r.raw) != `{"ref":"A1","state":"withdrawn"}` {
		t.Fatalf("got %v %s %v", r.a, r.raw, r.err)
	}
	want := "until=" + strings.ReplaceAll(until.UTC().Format(time.RFC3339Nano), ":", "%3A") + "&wait=1"
	if query != want {
		t.Errorf("query %q, want %q", query, want)
	}
	if time.Now().Before(until.Add(400 * time.Millisecond)) {
		t.Error("returned before the delayed answer")
	}
}

func TestPollAskStopsAtTheFirstClosedStateAndOnAnError(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	c := &pollClock{t: start, answerOn: 2}
	r := pollAsk(openRead(), start.Add(time.Hour), c.now, c.fetch)
	if r.err != nil || r.a.State != "answered" || string(r.raw) != `{"a":1}` || len(c.waits) != 2 {
		t.Errorf("got %v %s %v after %v", r.a, r.raw, r.err, c.waits)
	}
	closed := &pollClock{t: start}
	if r := pollAsk(read{a: askView{State: "answered"}}, start.Add(time.Hour), closed.now, closed.fetch); r.err != nil || len(closed.waits) != 0 {
		t.Errorf("an answered ask must not be waited on: %v %v", r.err, closed.waits)
	}
	failing := &pollClock{t: start, failOn: 2}
	if r := pollAsk(openRead(), start.Add(time.Hour), failing.now, failing.fetch); r.err == nil || len(failing.waits) != 2 {
		t.Errorf("got %v after %v", r.err, failing.waits)
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
