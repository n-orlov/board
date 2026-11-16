package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const finishLease = `{"ref":"L3","task":"aa-1","actor":"me","intent":"i","state":"ended","resources":[],"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z","outcome":"shipped"}`

func finishAnswer(done bool) string {
	d := "false"
	if done {
		d = "true"
	}
	return `{"lease":` + finishLease + `,"note":{"ref":"N1","summary":"shipped"},"done":` + d + `}`
}

func TestFinishSendsOneRequestAndPrintsTheLeaseLine(t *testing.T) {
	f := answering(200, finishAnswer(false))
	code, out, body := sentBody(t, f, "finish", "AA-1", "shipped it")
	r := f.requests[0]
	if code != ExitOK || r.Method != "POST" || r.URL.Path != "/api/v1/finish" || len(f.requests) != 1 {
		t.Fatalf("got %d %s %s", code, r.Method, r.URL.Path)
	}
	if out != "ok lease L3 ended\n" {
		t.Errorf("stdout %q", out)
	}
	if body["task"] != "AA-1" || body["outcome"] != "shipped it" || body["done"] != false || len(body) != 3 {
		t.Errorf("body %v", body)
	}
}

func TestFinishWithDoneAddsTheTaskDoneLineAndOmitsTheTaskWhenNoneIsGiven(t *testing.T) {
	f := answering(200, finishAnswer(true))
	code, out, body := sentBody(t, f, "finish", "all of it", "--done")
	if code != ExitOK || out != "ok lease L3 ended\nok task aa-1 done\n" {
		t.Errorf("got %d %q", code, out)
	}
	if body["outcome"] != "all of it" || body["done"] != true || body["task"] != nil || len(body) != 2 {
		t.Errorf("body %v", body)
	}
}

func TestFinishJSONPrintsTheAnswerAsItCame(t *testing.T) {
	answer := finishAnswer(true)
	f := answering(200, answer)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "finish", "x", "--json"); code != ExitOK || out != answer+"\n" || errs != "" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}

func TestFinishRefusalsWordTheirNext(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		code   int
		want   string
	}{
		{http.StatusNotFound, `{"detail":"there is no task aa-9"}`, ExitNotFound, "there is no task aa-9 → vh task ls\n"},
		{http.StatusConflict, `{"detail":"you hold no live lease","next":"Start one first."}`, ExitConflict, "you hold no live lease → Start one first.\n"},
		{http.StatusConflict, `{"detail":"cannot mark task aa-1 done: h holds lease L2 (x)","next":"wait for L2 to end, or --force"}`, ExitConflict,
			"cannot mark task aa-1 done: h holds lease L2 (x) → wait for the other lease to end, or vh task done aa-1 --force\n"},
		{http.StatusBadRequest, `{"detail":"bad"}`, ExitError, "bad\n"},
	} {
		f := answering(c.status, c.body)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), "finish", "x", "--done")
		srv.Close()
		if code != c.code || out != "" || errs != c.want {
			t.Errorf("%d: got %d %q %q, want %d %q", c.status, code, out, errs, c.code, c.want)
		}
	}
}

func TestFinishMalformedLinesAreUsageErrorsThatSendNothing(t *testing.T) {
	f := answering(200, finishAnswer(false))
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, args := range [][]string{
		{"finish"}, {"finish", "--done"}, {"finish", "a", "b", "c"}, {"finish", "x", "--no-such"}, {"finish", "x", "--done=1"},
	} {
		code, out, errs := runEnv(f.env(srv), args...)
		if code != ExitUsage || out != "" || errs != FinishUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("%d requests were sent for malformed command lines", len(f.requests))
	}
}

func TestFinishWithAnUnreadableAnswerOrNoServerFails(t *testing.T) {
	f := answering(200, `{"lease": "not an object"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, _ := runEnv(f.env(srv), "finish", "x"); code == ExitOK || out != "" {
		t.Errorf("unreadable answer: got %d %q", code, out)
	}
	none := func(string) string { return "" }
	if code, _, _ := runEnv(none, "finish", "x"); code == ExitOK {
		t.Error("finish with no server configured succeeded")
	}
}

func TestFinishIsInTheTreeWithItsFlags(t *testing.T) {
	got, ok := Commands()["finish"]
	if !ok || strings.Join(got, ",") != "--done,--json" {
		t.Errorf("Commands()[finish] = %v", got)
	}
}
