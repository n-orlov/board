package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSpanWritesHoursMinutesAndSeconds(t *testing.T) {
	for d, want := range map[time.Duration]string{
		2 * time.Hour: "2h", 3*time.Hour + 10*time.Minute: "3h10m", 41 * time.Minute: "41m",
		20 * time.Second: "20s", 0: "0s", -time.Minute: "0s", 59*time.Minute + 59*time.Second + 600*time.Millisecond: "1h",
	} {
		if got := span(d); got != want {
			t.Errorf("span(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestEndArgsTellTheLeaseFromTheOutcome(t *testing.T) {
	cases := []struct {
		pos             []string
		target, outcome string
		ok              bool
	}{
		{nil, "", "", true},
		{[]string{"L7"}, "L7", "", true},
		{[]string{"aa-1"}, "aa-1", "", true},
		{[]string{"shipped it"}, "", "shipped it", true},
		{[]string{"done"}, "", "done", true},
		{[]string{"aa-1", "shipped"}, "aa-1", "shipped", true},
		{[]string{"a", "b", "c"}, "", "", false},
	}
	for _, c := range cases {
		target, outcome, ok := endArgs(c.pos)
		if target != c.target || outcome != c.outcome || ok != c.ok {
			t.Errorf("%v: got %q %q %v", c.pos, target, outcome, ok)
		}
	}
}

func TestLeaseLineByState(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	base := leaseView{Ref: "L7", Task: "aa-1", Actor: "me", Intent: `say "hi"`, Resources: []string{"a:b", "c:d"}}
	for _, c := range []struct {
		state, ended, want string
	}{
		{"live", "", `L7  live  aa-1  me  "say \"hi\""  res a:b,c:d  exp 41m`},
		{"expired", "", `L7  expired  aa-1  me  "say \"hi\""  res a:b,c:d  expired 19m ago`},
		{"ended", "2026-03-04T11:50:00Z", `L7  ended  aa-1  me  "say \"hi\""  res a:b,c:d  ended 10m ago`},
	} {
		l := base
		l.State, l.EndedAt = c.state, c.ended
		l.ExpiresAt = map[string]string{"live": "2026-03-04T12:41:00Z", "expired": "2026-03-04T11:41:00Z", "ended": "2026-03-04T13:00:00Z"}[c.state]
		if got := leaseLine(l, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.state, got, c.want)
		}
	}
	bare := leaseView{Ref: "L1", State: "live", Task: "t-1", Actor: "me", Intent: "x", ExpiresAt: "soon"}
	if got := leaseLine(bare, now); got != `L1  live  t-1  me  "x"  exp soon` {
		t.Errorf("got %q", got)
	}
}

func TestConflictLineAndTTLFallBackOnUnreadableTimes(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	c := conflictView{Lease: "L5", Holder: "h", Task: "bb-2", Intent: "go", ExpiresAt: "2026-03-04T12:41:00Z", Resources: []string{"host:gw1"}}
	if got := conflictLine(c, now); got != `CONFLICT host:gw1: L5 h (bb-2) "go" exp 41m` {
		t.Errorf("got %q", got)
	}
	c.ExpiresAt = "later"
	if got := conflictLine(c, now); !strings.HasSuffix(got, "exp later") {
		t.Errorf("got %q", got)
	}
	if got := leaseTTL(leaseView{RenewedAt: "x", ExpiresAt: "y"}); got != "y" {
		t.Errorf("got %q", got)
	}
}

func TestExpiredDetailNamesTheLeaseAndHowLongAgo(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	ae := &apiError{Detail: "server words", ExpiredAt: "2026-03-04T11:46:00Z"}
	if got := expiredDetail(ae, "L7", now); got != "lease L7 expired 14m ago" {
		t.Errorf("got %q", got)
	}
	ae.ExpiredAt = ""
	if got := expiredDetail(ae, "L7", now); got != "server words" {
		t.Errorf("got %q", got)
	}
}

const leaseAnswer = `{"lease":{"ref":"L1","task":"aa-1","actor":"claude:test","intent":"i","state":"live","resources":[],"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z"},"conflicts":[]}`

func TestLeaseStartSendsTheTaskIntentTtlAndResources(t *testing.T) {
	f := answering(201, leaseAnswer)
	code, out, body := sentBody(t, f, "lease", "start", "AA-1", "doing it", "--ttl", "4h", "--res", "repo:x", "--res=host:y")
	r := f.requests[0]
	if code != ExitOK || out != "ok lease L1 aa-1 exp 2h\n" || r.Method != "POST" || r.URL.Path != "/api/v1/leases" {
		t.Fatalf("got %d %q %s %s", code, out, r.Method, r.URL.Path)
	}
	res, _ := body["resources"].([]any)
	if body["task"] != "AA-1" || body["intent"] != "doing it" || body["ttl_seconds"] != float64(14400) || len(res) != 2 {
		t.Errorf("body %v", body)
	}
}

func TestLeaseStartRefusalsWordTheirNext(t *testing.T) {
	for _, c := range []struct {
		status int
		code   int
		want   string
	}{
		{http.StatusNotFound, ExitNotFound, "no task → vh task ls\n"},
		{http.StatusConflict, ExitConflict, "no task → vh task show aa-1\n"},
		{http.StatusBadRequest, ExitError, "no task\n"},
	} {
		f := answering(c.status, `{"detail":"no task"}`)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), "lease", "start", "AA-1", "x")
		srv.Close()
		if code != c.code || out != "" || errs != c.want {
			t.Errorf("%d: got %d %q %q", c.status, code, out, errs)
		}
	}
	done := startFailure(&apiError{Status: http.StatusConflict, Detail: "cannot start a lease on task aa-1: it is done"}, "AA-1")
	var ae *apiError
	if !errors.As(done, &ae) || ae.Next != "vh task reopen aa-1 first" || ae.exitCode() != ExitConflict {
		t.Errorf("done task: got %v", done)
	}
	if got := startFailure(http.ErrServerClosed, "aa-1"); !errors.Is(got, http.ErrServerClosed) {
		t.Errorf("got %v", got)
	}
}

func TestLeaseConflictsPrintAndExitSeven(t *testing.T) {
	answer := strings.Replace(leaseAnswer, `"conflicts":[]`, `"conflicts":[{"lease":"L5","holder":"h","task":"bb-1","intent":"go","expires_at":"2026-03-04T14:00:00Z","resources":["a:b"]}]`, 1)
	f := answering(200, answer)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "lease", "start", "aa-1", "i")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != ExitWarning || len(lines) != 2 || !strings.HasPrefix(lines[1], "CONFLICT a:b: L5 h (bb-1) \"go\" exp ") {
		t.Errorf("got %d %q", code, out)
	}
	code, out, _ = runEnv(f.env(srv), "lease", "start", "aa-1", "i", "--json")
	if code != ExitWarning || out != answer+"\n" {
		t.Errorf("json: got %d %q", code, out)
	}
}

func TestLeaseRenewWithNoArgumentRenewsEveryLiveLeaseOfMine(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"items":[{"ref":"L1"},{"ref":"L2"}]}`))
			return
		}
		_, _ = w.Write([]byte(leaseAnswer))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "lease", "renew", "--ttl", "1h", "--intent", "again")
	if code != ExitOK || errs != "" || strings.Count(out, "ok lease") != 2 {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	get := f.requests[0].URL.Query()
	if get.Get("actor") != "claude:test" || get.Get("state") != "live" {
		t.Errorf("list query %v", get)
	}
	if f.requests[1].URL.Path != "/api/v1/leases/L1/renew" || f.requests[2].URL.Path != "/api/v1/leases/L2/renew" ||
		!strings.Contains(f.bodies[1], `"ttl_seconds":3600`) || !strings.Contains(f.bodies[1], `"intent":"again"`) {
		t.Errorf("requests %v %v", f.requests[1].URL.Path, f.bodies)
	}
	_, out, _ = runEnv(f.env(srv), "lease", "renew", "--json")
	if !strings.HasPrefix(out, "[") {
		t.Errorf("json of many is an array: %q", out)
	}
}

func TestLeaseRenewByTaskPicksMyLatestLease(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"items":[{"ref":"L1"},{"ref":"L4"}]}`))
			return
		}
		_, _ = w.Write([]byte(leaseAnswer))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, _, _ := runEnv(f.env(srv), "lease", "renew", "aa-1")
	if code != ExitOK || f.requests[0].URL.Query().Get("task") != "aa-1" || f.requests[0].URL.Query().Get("state") != "all" ||
		f.requests[1].URL.Path != "/api/v1/leases/L4/renew" || f.bodies[1] != "" {
		t.Errorf("got %d %v %q", code, f.requests[1].URL.Path, f.bodies)
	}
}

func TestLeaseRenewOfAnExpiredLeaseExitsSixWithTheStart(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"ref":"L1","task":"aa-1"}`))
			return
		}
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"detail":"lease L1 expired at x","expired_at":"2026-03-04T11:46:00Z"}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	was := nowFn
	t.Cleanup(func() { nowFn = was })
	nowFn = func() time.Time { return time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) }
	code, out, errs := runEnv(f.env(srv), "lease", "renew", "l1")
	if code != ExitConflict || out != "" || errs != "lease L1 expired 14m ago → vh lease start aa-1 \"<intent>\"\n" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}

func TestLeaseRenewAndEndRefusals(t *testing.T) {
	for _, c := range []struct {
		args   []string
		status int
		code   int
		want   string
	}{
		{[]string{"renew", "L1"}, http.StatusNotFound, ExitNotFound, "gone → vh lease ls\n"},
		{[]string{"renew", "L1"}, http.StatusConflict, ExitConflict, "gone → vh lease start <task> \"<intent>\"\n"},
		{[]string{"renew", "L1"}, http.StatusBadRequest, ExitError, "gone\n"},
		{[]string{"end", "L1"}, http.StatusNotFound, ExitNotFound, "gone → vh lease ls\n"},
		{[]string{"end", "L1"}, http.StatusConflict, ExitConflict, "gone → vh lease ls --mine --state all\n"},
		{[]string{"end", "L1"}, http.StatusBadRequest, ExitError, "gone\n"},
		{[]string{"ack", "L1"}, http.StatusNotFound, ExitNotFound, "gone → vh lease ls\n"},
		{[]string{"ack", "L1"}, http.StatusConflict, ExitConflict, "gone → vh lease ls --mine --state all\n"},
		{[]string{"ack", "L1"}, http.StatusBadRequest, ExitError, "gone\n"},
	} {
		f := answering(c.status, `{"detail":"gone"}`)
		srv := httptest.NewServer(f)
		code, _, errs := runEnv(f.env(srv), append([]string{"lease"}, c.args...)...)
		srv.Close()
		if code != c.code || errs != c.want {
			t.Errorf("%v %d: got %d %q", c.args, c.status, code, errs)
		}
	}
	for _, err := range []error{http.ErrServerClosed} {
		if got := endFailure(err); !errors.Is(got, err) {
			t.Errorf("got %v", got)
		}
		if got := (&client{}).renewFailure(context.Background(), err, "L1"); !errors.Is(got, err) {
			t.Errorf("got %v", got)
		}
	}
}

func TestLeaseEndSendsTheOutcomeAndPrintsOneLine(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"items":[{"ref":"L3"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ref":"L3","state":"ended"}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "lease", "end", "shipped it")
	if code != ExitOK || out != "ok lease L3 ended\n" || errs != "" || f.requests[1].URL.Path != "/api/v1/leases/L3/end" || f.bodies[1] != `{"outcome":"shipped it"}` {
		t.Errorf("got %d %q %q %q %s", code, out, errs, f.bodies, f.requests[1].URL.Path)
	}
}

func TestLeaseEndWithSeveralLiveLeasesAndNoArgumentIsUsage(t *testing.T) {
	f := answering(200, `{"items":[{"ref":"L1"},{"ref":"L2"}]}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, _, errs := runEnv(f.env(srv), "lease", "end")
	if code != ExitUsage || errs != "you hold 2 live leases: L1, L2 → vh lease end <L…>\n" {
		t.Errorf("got %d %q", code, errs)
	}
}

func TestLeaseTargetsWithNothingToFind(t *testing.T) {
	f := answering(200, `{"items":[]}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"renew"}, "you hold no live lease → vh lease ls\n"},
		{[]string{"renew", "aa-1"}, "you hold no lease on task aa-1 → vh lease ls --task aa-1\n"},
		{[]string{"end"}, "you hold no live lease → vh lease ls\n"},
	} {
		code, _, errs := runEnv(f.env(srv), append([]string{"lease"}, c.args...)...)
		if code != ExitNotFound || errs != c.want {
			t.Errorf("%v: got %d %q", c.args, code, errs)
		}
	}
	if got := targetFailure(http.ErrServerClosed); !errors.Is(got, http.ErrServerClosed) {
		t.Errorf("got %v", got)
	}
}

func TestLeaseLsFiltersCapAndJSON(t *testing.T) {
	items := make([]string, 25)
	for i := range items {
		items[i] = `{"ref":"L1","state":"live","task":"aa-1","actor":"a","intent":"x","expires_at":"2099-01-01T00:00:00Z"}`
	}
	f := answering(200, `{"items":[`+strings.Join(items, ",")+`]}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "lease", "ls", "--mine", "--task", "aa 1", "--res", "r:x", "--state", "all")
	q := f.requests[0].URL.Query()
	if code != ExitOK || strings.Count(out, "\n") != 21 || !strings.HasSuffix(out, "+5 more → vh lease ls --state all --mine --task \"aa 1\" --res r:x --all\n") ||
		q.Get("actor") != "claude:test" || q.Get("task") != "aa 1" || q.Get("resource") != "r:x" || q.Get("state") != "all" {
		t.Errorf("got %d %q %v", code, out, q)
	}
	_, out, _ = runEnv(f.env(srv), "lease", "ls", "--json", "--all")
	if !strings.HasPrefix(out, "[") || strings.Count(out, `"ref"`) != 25 || f.requests[1].URL.Query().Get("state") != "live" {
		t.Errorf("json: %q", out[:40])
	}
}

func TestLeaseLsUnknownTaskExitsFour(t *testing.T) {
	f := answering(404, `{"detail":"there is no task zz-1"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, _, errs := runEnv(f.env(srv), "lease", "ls", "--task", "zz-1")
	if code != ExitNotFound || errs != "there is no task zz-1 → vh task ls\n" {
		t.Errorf("got %d %q", code, errs)
	}
}

func TestLeaseUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"lease"}, {"lease", "frob"}, {"lease", "start"}, {"lease", "start", "a"}, {"lease", "start", "a", "b", "c"},
		{"lease", "renew", "a", "b"}, {"lease", "renew", "--bogus"}, {"lease", "end", "a", "b", "c"},
		{"lease", "ack"}, {"lease", "ack", "aa-1"}, {"lease", "ack", "L1", "L2"}, {"lease", "ack", "L1", "--bogus"},
		{"lease", "ls", "x"}, {"lease", "ls", "--state", "weird"}, {"lease", "ls", "--all", "--limit", "3"},
	} {
		code, out, errs := runEnv(envWith(map[string]string{}), args...)
		if code != ExitUsage || out != "" || errs != LeaseUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	for _, args := range [][]string{{"lease", "start", "a", "b", "--ttl", "2"}, {"lease", "renew", "--ttl", "x"}} {
		code, out, errs := runEnv(envWith(map[string]string{}), args...)
		if code != ExitUsage || out != "" || !strings.HasPrefix(errs, "vh: --ttl: invalid duration") {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestLeaseAckPostsToTheLeaseAndPrintsOneLine(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ref":"L3","state":"expired"}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "lease", "ack", "l3")
	if code != ExitOK || out != "ok lease L3 acknowledged\n" || errs != "" || len(f.requests) != 1 || f.requests[0].Method != "POST" || f.requests[0].URL.Path != "/api/v1/leases/L3/ack" {
		t.Errorf("got %d %q %q %v", code, out, errs, f.requests)
	}
	code, out, _ = runEnv(f.env(srv), "lease", "ack", "L3", "--json")
	if code != ExitOK || out != `{"ref":"L3","state":"expired"}`+"\n" {
		t.Errorf("--json: got %d %q", code, out)
	}
}

func TestLeaseAckReportsAnUnreadableAnswer(t *testing.T) {
	f := answering(http.StatusOK, `"not a lease"`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "lease", "ack", "L3")
	if code != ExitError || out != "" || errs == "" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}
