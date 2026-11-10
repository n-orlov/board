package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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

// lateFetch returns, on its first call, the ask as ended at the given offset
// from start, the clock having moved to the deadline.
func lateFetch(start time.Time, state string, endedAt time.Duration) askFetch {
	return func(_ int, _ time.Duration) (askView, json.RawMessage, error) {
		stamp := start.Add(endedAt).Format(time.RFC3339Nano)
		a := askView{Ref: "A1", State: state, AnsweredAt: stamp}
		if state == "withdrawn" {
			a = askView{Ref: "A1", State: state, WithdrawnAt: stamp}
		}
		return a, json.RawMessage(`{"read":"after"}`), nil
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
		a, _, err := pollAsk(askView{Ref: "A1", State: "open"}, nil, deadline, clock.now, lateFetch(start, c.state, c.at))
		if err != nil || a.State != c.want || pollExit(a.State) != c.code {
			t.Errorf("%s at %s: got %q exit %d (%v), want %q exit %d", c.state, c.at, a.State, pollExit(a.State), err, c.want, c.code)
		}
	}
}

// A deadline with a sub-millisecond part shares its millisecond with stamps of
// ends made both before and after it, since the server floors a stamp to the
// millisecond: only a stamp whose whole millisecond is over by the deadline
// counts, and a stamp carrying its own sub-millisecond part is taken as exact.
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
		a := askView{Ref: "A1", State: c.state, AnsweredAt: c.stamp}
		if c.state == "withdrawn" {
			a = askView{Ref: "A1", State: c.state, WithdrawnAt: c.stamp}
		}
		for _, read := range []struct {
			at   time.Time
			code int
		}{
			{deadline, c.code}, // the read comes back at the deadline: the stamp decides
			{deadline.Add(time.Microsecond), ExitTimeout}, // after it: the end may have committed late, whatever the stamp
		} {
			clock := &pollClock{t: start}
			fetch := func(_ int, _ time.Duration) (askView, json.RawMessage, error) {
				clock.t = read.at
				return a, nil, nil
			}
			got, _, err := pollAsk(askView{Ref: "A1", State: "open"}, nil, deadline, clock.now, fetch)
			if err != nil || pollExit(got.State) != read.code {
				t.Errorf("%s at %s read at %s: got %q exit %d (%v), want exit %d", c.state, c.stamp,
					read.at.Format(time.RFC3339Nano), got.State, pollExit(got.State), err, read.code)
			}
		}
	}
}

// An end read by a call that came back after the deadline is not counted even
// when its stamp is well before the deadline: the stamp is taken before the
// transaction commits, so the ask may still have been open at the deadline.
func TestPollAskDoesNotCountAnEndReadAfterTheDeadline(t *testing.T) {
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(10 * time.Second)
	for _, c := range []struct {
		state  string
		stamp  time.Duration // from start
		readAt time.Duration // when the call returns, from start
		code   int
	}{
		{"answered", 9 * time.Second, 10 * time.Second, ExitOK},
		{"withdrawn", 9 * time.Second, 10 * time.Second, ExitConflict},
		{"answered", 9 * time.Second, 10*time.Second + time.Nanosecond, ExitTimeout},
		{"withdrawn", 9 * time.Second, 10*time.Second + time.Nanosecond, ExitTimeout},
		{"answered", time.Second, time.Minute, ExitTimeout},
		{"withdrawn", 5 * time.Second, 10*time.Second + 75*time.Millisecond, ExitTimeout},
	} {
		stamp := start.Add(c.stamp).Format(time.RFC3339Nano)
		a := askView{Ref: "A1", State: c.state, AnsweredAt: stamp}
		if c.state == "withdrawn" {
			a = askView{Ref: "A1", State: c.state, WithdrawnAt: stamp}
		}
		clock := &pollClock{t: start}
		calls := 0
		fetch := func(_ int, _ time.Duration) (askView, json.RawMessage, error) {
			calls++
			clock.t = start.Add(c.readAt)
			return a, json.RawMessage(`{"read":1}`), nil
		}
		got, raw, err := pollAsk(askView{Ref: "A1", State: "open"}, nil, deadline, clock.now, fetch)
		if err != nil || pollExit(got.State) != c.code || calls != 1 || string(raw) != `{"read":1}` {
			t.Errorf("%s stamped %s read at %s: got %q exit %d raw %s after %d calls (%v), want exit %d",
				c.state, c.stamp, c.readAt, got.State, pollExit(got.State), raw, calls, err, c.code)
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
	fetch := func(_ int, _ time.Duration) (askView, json.RawMessage, error) {
		calls++
		return askView{Ref: "A1", State: "answered", AnsweredAt: start.Add(901 * time.Millisecond).Format(time.RFC3339Nano)}, nil, nil
	}
	clock := &pollClock{t: start}
	a, _, err := pollAsk(askView{Ref: "A1", State: "open"}, nil, start.Add(900*time.Millisecond), clock.now, fetch)
	if err != nil || a.State != "open" || calls != 1 {
		t.Errorf("got %v after %d calls (%v)", a, calls, err)
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

// heldAskServer holds every ?wait= request until the client goes (an open ask
// nobody answers) and serves each plain read from plain, recording the requests.
func heldAskServer(t *testing.T, plain func(n int, w http.ResponseWriter)) (*client, *[]string) {
	var mu sync.Mutex
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		kind := "plain"
		if r.URL.Query().Get("wait") != "" {
			kind = "wait=" + r.URL.Query().Get("wait")
		}
		reqs = append(reqs, kind)
		n := len(reqs)
		mu.Unlock()
		if kind != "plain" {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		plain(n, w)
	}))
	t.Cleanup(srv.Close)
	return &client{base: srv.URL, http: srv.Client(), key: "k"}, &reqs
}

// The deadline cuts a held wait off and the ask is read once more after it: the
// result is that read's ask, whose ended_at pollAsk judges, and not the one the
// plain read before the deadline saw.
func TestWaitFetchReadsTheAskAgainAfterTheDeadline(t *testing.T) {
	c, reqs := heldAskServer(t, func(n int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"ref":"A1","state":"open","n":` + strconv.Itoa(n) + `}`))
	})
	started := time.Now()
	a, raw, err := waitFetch(context.Background(), c, "A1")(1, 300*time.Millisecond)
	took := time.Since(started)
	if err != nil || a.State != "open" || string(raw) != `{"ref":"A1","state":"open","n":3}` {
		t.Fatalf("got %v %s %v", a, raw, err)
	}
	if took < 300*time.Millisecond || took > 2*time.Second {
		t.Errorf("returned after %s, want at the deadline", took)
	}
	if len(*reqs) != 3 || (*reqs)[0] != "wait=1" || (*reqs)[1] != "plain" || (*reqs)[2] != "plain" {
		t.Errorf("requests %v", *reqs)
	}
}

// When the read after the deadline fails, the result is the ask the plain read
// before the deadline saw open; when that one failed too, the error.
func TestWaitFetchFallsBackToTheProbeWhenTheLastReadFails(t *testing.T) {
	c, _ := heldAskServer(t, func(n int, w http.ResponseWriter) {
		if n == 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ref":"A1","state":"open"}`))
	})
	if a, raw, err := waitFetch(context.Background(), c, "A1")(1, 300*time.Millisecond); err != nil || a.State != "open" || string(raw) != `{"ref":"A1","state":"open"}` {
		t.Errorf("got %v %s %v", a, raw, err)
	}
	failing, _ := heldAskServer(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) })
	if _, _, err := waitFetch(context.Background(), failing, "A1")(1, 300*time.Millisecond); err == nil {
		t.Error("both reads failed, want an error")
	}
}

// A plain read made before the deadline finds an end the held wait never
// delivers (its response is delayed past the deadline): that end is the result,
// at the time of the read, not at the deadline.
func TestWaitFetchFindsAnEndTheHeldWaitDeliversLate(t *testing.T) {
	for _, state := range []string{"answered", "withdrawn"} {
		c, _ := heldAskServer(t, func(_ int, w http.ResponseWriter) {
			_, _ = w.Write([]byte(`{"ref":"A1","state":"` + state + `"}`))
		})
		started := time.Now()
		a, _, err := waitFetch(context.Background(), c, "A1")(2, 1500*time.Millisecond)
		took := time.Since(started)
		if err != nil || a.State != state {
			t.Fatalf("%s: got %v %v", state, a, err)
		}
		if took < 1200*time.Millisecond || took > 1450*time.Millisecond {
			t.Errorf("%s: returned after %s, want 200 ms before the deadline", state, took)
		}
	}
}

// A plain read before the deadline that fails leaves the read after it, so the
// result is still the ask as it is then.
func TestWaitFetchReadsAfterTheDeadlineWhenTheProbeFailed(t *testing.T) {
	c, reqs := heldAskServer(t, func(n int, w http.ResponseWriter) {
		if n == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ref":"A1","state":"answered"}`))
	})
	a, _, err := waitFetch(context.Background(), c, "A1")(1, 300*time.Millisecond)
	if err != nil || a.State != "answered" || len(*reqs) != 3 || (*reqs)[2] != "plain" {
		t.Errorf("got %v %v after %v", a, err, *reqs)
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

// endedInTime counts an end by its ended_at whenever it was read, by the time
// the read came back otherwise, and never an open ask or an ended_at after the
// deadline.
func TestEndedInTimeTrustsEndedAtOverTheReadTime(t *testing.T) {
	deadline := time.Date(2026, 3, 4, 12, 0, 1, 0, time.UTC)
	stamp := deadline.Add(-time.Second).Format(time.RFC3339Nano)
	late := deadline.Add(time.Hour)
	for _, c := range []struct {
		name   string
		a      askView
		readAt time.Time
		want   bool
	}{
		{"ended_at at the deadline, read late", askView{State: "answered", AnsweredAt: stamp, EndedAt: "2026-03-04T12:00:01Z"}, late, true},
		{"ended_at before the deadline, read late", askView{State: "withdrawn", WithdrawnAt: stamp, EndedAt: "2026-03-04T12:00:00.999Z"}, late, true},
		{"ended_at 1ms after the deadline, read late", askView{State: "answered", AnsweredAt: stamp, EndedAt: "2026-03-04T12:00:01.001Z"}, late, false},
		{"ended_at after the deadline, read in time", askView{State: "answered", AnsweredAt: stamp, EndedAt: "2026-03-04T12:00:01.001Z"}, deadline, true},
		{"no ended_at, read late", askView{State: "withdrawn", WithdrawnAt: stamp}, late, false},
		{"no ended_at, read in time", askView{State: "withdrawn", WithdrawnAt: stamp}, deadline, true},
		{"unreadable ended_at, read late", askView{State: "answered", AnsweredAt: stamp, EndedAt: "soon"}, late, false},
		{"open with an ended_at", askView{State: "open", EndedAt: "2026-03-04T12:00:00Z"}, late, false},
	} {
		if got := endedInTime(c.a, deadline, c.readAt); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
