package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pingLease(ref, task string) string {
	return `{"ref":"` + ref + `","task":"` + task + `","actor":"me","intent":"i","state":"live","resources":[],"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z"}`
}

const pingOne = `{"leases":[` + `{"ref":"L3","task":"aa-1","actor":"me","intent":"i","state":"live","resources":[],"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z"}` + `],"milestones":[]}`

func TestPingSendsOneRequestWithNoBodyFieldsAndPrintsTheRenewLine(t *testing.T) {
	f := answering(200, pingOne)
	code, out, body := sentBody(t, f, "ping")
	r := f.requests[0]
	if code != ExitOK || r.Method != "POST" || r.URL.Path != "/api/v1/ping" || len(f.requests) != 1 {
		t.Fatalf("got %d %s %s", code, r.Method, r.URL.Path)
	}
	if out != "ok lease L3 aa-1 exp 2h\n" {
		t.Errorf("stdout %q", out)
	}
	if len(body) != 0 {
		t.Errorf("body %v, want none of progress", body)
	}
}

func TestPingWithProgressPrintsALeaseLineAndAMilestoneLineForEachTask(t *testing.T) {
	f := answering(200, `{"leases":[`+pingLease("L1", "aa-1")+`,`+pingLease("L2", "aa-2")+`,`+pingLease("L4", "bb-7")+`],"milestones":["aa-1","aa-2","bb-7"]}`)
	code, out, body := sentBody(t, f, "ping", "tests green")
	want := "ok lease L1 aa-1 exp 2h\nok lease L2 aa-2 exp 2h\nok lease L4 bb-7 exp 2h\n" +
		"ok task aa-1 milestone recorded\nok task aa-2 milestone recorded\nok task bb-7 milestone recorded\n"
	if code != ExitOK || out != want {
		t.Errorf("got %d %q, want %q", code, out, want)
	}
	if body["progress"] != "tests green" || len(body) != 1 {
		t.Errorf("body %v", body)
	}
}

func TestPingJSONPrintsTheAnswerAsItCame(t *testing.T) {
	f := answering(200, pingOne)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "ping", "--json"); code != ExitOK || out != pingOne+"\n" || errs != "" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}

func TestPingRefusalsWordTheirNext(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		code   int
		want   string
	}{
		{http.StatusConflict, `{"detail":"you hold no live lease","next":"Start one first."}`, ExitConflict, "you hold no live lease → Start one first.\n"},
		{http.StatusBadRequest, `{"detail":"bad"}`, ExitError, "bad\n"},
	} {
		f := answering(c.status, c.body)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), "ping", "x")
		srv.Close()
		if code != c.code || out != "" || errs != c.want {
			t.Errorf("%d: got %d %q %q, want %d %q", c.status, code, out, errs, c.code, c.want)
		}
	}
}

func TestPingMalformedLinesAreUsageErrorsThatSendNothing(t *testing.T) {
	f := answering(200, pingOne)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, args := range [][]string{{"ping", "a", "b"}, {"ping", "--no-such"}, {"ping", "--json=1"}} {
		code, out, errs := runEnv(f.env(srv), args...)
		if code != ExitUsage || out != "" || errs != PingUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("%d requests were sent for malformed command lines", len(f.requests))
	}
}

func TestPingWithAnUnreadableAnswerOrNoServerFails(t *testing.T) {
	f := answering(200, `{"leases": "not an array"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, _ := runEnv(f.env(srv), "ping"); code == ExitOK || out != "" {
		t.Errorf("unreadable answer: got %d %q", code, out)
	}
	none := func(string) string { return "" }
	if code, _, _ := runEnv(none, "ping"); code == ExitOK {
		t.Error("ping with no server configured succeeded")
	}
}

func TestPingIsInTheTreeWithItsFlags(t *testing.T) {
	got, ok := Commands()["ping"]
	if !ok || strings.Join(got, ",") != "--json" {
		t.Errorf("Commands()[ping] = %v", got)
	}
}
