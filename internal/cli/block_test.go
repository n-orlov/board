package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	blockAskOpen   = `"ref":"A1","task":"aa-1","asker":"me","question":"Which port?","details":"","options":[],"mode":"%s","blocking":false,"state":"open","created_at":"2026-03-04T12:00:00Z","asker_waiting":false`
	blockLeaseLive = `{"ref":"L1","task":"aa-1","actor":"me","intent":"i","state":"live","resources":[],"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z"}`
	blockLeaseEnd  = `{"ref":"L1","task":"aa-1","actor":"me","intent":"i","state":"ended","outcome":"blocked on A1","resources":[],"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z"}`
)

func raised(mode, lease string) string {
	body := `{` + strings.Replace(blockAskOpen, "%s", mode, 1)
	if lease != "" {
		body += `,"lease":` + lease
	}
	return body + `}`
}

func TestBlockSendsAStopAskAndPrintsTheAskLineThenTheLeaseLine(t *testing.T) {
	f := answering(201, raised("stop", blockLeaseEnd))
	code, out, body := sentBody(t, f, "block", "aa-1", "Which port?")
	r := f.requests[0]
	if code != ExitOK || r.Method != "POST" || r.URL.Path != "/api/v1/asks" || len(f.requests) != 1 {
		t.Fatalf("got %d %s %s", code, r.Method, r.URL.Path)
	}
	if out != "ok ask A1 raised\nok lease L1 ended\n" {
		t.Errorf("stdout %q", out)
	}
	want := map[string]any{"task": "aa-1", "question": "Which port?", "mode": "stop", "lease_action": "stop"}
	if len(body) != len(want) || body["task"] != want["task"] || body["question"] != want["question"] || body["mode"] != "stop" || body["lease_action"] != "stop" {
		t.Errorf("body %v, want %v", body, want)
	}
}

func TestBlockWithoutALeasePrintsOnlyTheAskLine(t *testing.T) {
	for _, args := range [][]string{{"block", "aa-1", "Which port?"}, {"block", "aa-1", "Which port?", "--stop"}} {
		code, out, _ := sentBody(t, answering(201, raised("stop", "")), args...)
		if code != ExitOK || out != "ok ask A1 raised\n" {
			t.Errorf("%v: got %d %q", args, code, out)
		}
	}
}

func TestBlockJSONPrintsTheAnswerAsItCame(t *testing.T) {
	answer := raised("stop", blockLeaseEnd)
	f := answering(201, answer)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "block", "aa-1", "Which port?", "--json"); code != ExitOK || out != answer+"\n" || errs != "" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}

func TestBlockRefusalsWordTheirNext(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		code   int
		want   string
	}{
		{http.StatusNotFound, `{"detail":"there is no task zz-9"}`, 4, "→ vh task ls"},
		{http.StatusConflict, `{"detail":"cannot raise an ask on task aa-2: it is closed","next":"Reopen it."}`, ExitConflict, "→ vh task show aa-1"},
	} {
		for _, args := range [][]string{{"block", "aa-1", "q"}, {"block", "aa-1", "q", "--poll", "2s"}} {
			f := answering(c.status, c.body)
			srv := httptest.NewServer(f)
			code, out, errs := runEnv(f.env(srv), args...)
			srv.Close()
			if code != c.code || out != "" || !strings.Contains(errs, c.want) || len(f.requests) != 1 {
				t.Errorf("%v with %d: got %d %q %q after %d requests, want %d and %q", args, c.status, code, out, errs, len(f.requests), c.code, c.want)
			}
		}
	}
}

func TestBlockUsageErrorsSendNothing(t *testing.T) {
	for _, args := range [][]string{
		{"block"}, {"block", "aa-1"}, {"block", "aa-1", "q", "extra"}, {"block", "aa-1", "q", "--no-such"},
		{"block", "aa-1", "q", "--poll", "5m", "--stop"}, {"block", "aa-1", "q", "--poll", "soon"}, {"block", "aa-1", "q", "--poll"},
	} {
		f := answering(201, raised("stop", ""))
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), args...)
		srv.Close()
		if code != ExitUsage || out != "" || strings.TrimSpace(errs) != BlockUsage || len(f.requests) != 0 {
			t.Errorf("%v: got %d %q %q after %d requests, want the usage line", args, code, out, errs, len(f.requests))
		}
	}
}

// blockBoard answers the raise of a poll ask, the renewals of L1 (status
// renewal) and the reads of A1 with read.
func blockBoard(lease string, renewal int, read func() string) *fakeBoard {
	return &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/asks":
			w.WriteHeader(201)
			_, _ = w.Write([]byte(raised("poll", lease)))
		case r.Method == "POST" && r.URL.Path == "/api/v1/leases/L1/renew":
			w.WriteHeader(renewal)
			_, _ = w.Write([]byte(blockLeaseLive))
		case r.Method == "GET" && r.URL.Path == "/api/v1/asks/A1":
			_, _ = w.Write([]byte(read()))
		default:
			w.WriteHeader(404)
		}
	}}
}

func endedAsk(state, extra string) func() string {
	return func() string {
		s := strings.Replace(`{`+strings.Replace(blockAskOpen, "%s", "poll", 1)+extra+`}`, `"state":"open"`, `"state":"`+state+`"`, 1)
		return s
	}
}

func paths(f *fakeBoard) []string {
	var got []string
	for _, r := range f.requests {
		got = append(got, r.Method+" "+r.URL.Path)
	}
	return got
}

func TestBlockPollKeepsTheLeaseRenewsItAndPrintsTheAnswer(t *testing.T) {
	f := blockBoard(blockLeaseLive, 200, endedAsk("answered", `,"answer":"8080","answered_by":"nik@phone","answered_at":"2026-03-04T12:00:05Z"`))
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "block", "aa-1", "Which port?", "--poll", "30s")
	if code != ExitOK || out != "ok ask A1 raised\nok A1 answered by nik@phone: 8080\n" || errs != "" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	if got := strings.Join(paths(f), ","); got != "POST /api/v1/asks,POST /api/v1/leases/L1/renew,GET /api/v1/asks/A1" {
		t.Errorf("requests %s, want the raise, one renewal and one read", got)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(f.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["mode"] != "poll" || sent["lease_action"] != "keep" || sent["task"] != "aa-1" || sent["question"] != "Which port?" {
		t.Errorf("raise body %v, want mode poll and lease_action keep", sent)
	}
}

func TestBlockPollWithoutALeaseRenewsNothing(t *testing.T) {
	f := blockBoard("", 200, endedAsk("withdrawn", `,"withdraw_reason":"moot","withdrawn_at":"2026-03-04T12:00:05Z"`))
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "block", "aa-1", "Which port?", "--poll", "30s")
	if code != ExitConflict || out != "ok ask A1 raised\nA1 withdrawn: moot\n" {
		t.Fatalf("got %d %q", code, out)
	}
	if got := strings.Join(paths(f), ","); got != "POST /api/v1/asks,GET /api/v1/asks/A1" {
		t.Errorf("requests %s, want no renewal", got)
	}
}

func TestBlockPollGoesOnWhenARenewalFails(t *testing.T) {
	f := blockBoard(blockLeaseLive, 409, endedAsk("answered", `,"answer":"8080","answered_by":"nik@phone","answered_at":"2026-03-04T12:00:05Z"`))
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "block", "aa-1", "Which port?", "--poll", "30s")
	if code != ExitOK || out != "ok ask A1 raised\nok A1 answered by nik@phone: 8080\n" || errs != "" {
		t.Errorf("got %d %q %q, want the answer despite the failed renewal", code, out, errs)
	}
}

func TestBlockPollRenewsBeforeEveryWaitAndExits3AtTheDeadline(t *testing.T) {
	f := blockBoard(blockLeaseLive, 200, func() string {
		time.Sleep(300 * time.Millisecond)
		return raised("poll", "")
	})
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "block", "aa-1", "Which port?", "--poll", "1s")
	if code != ExitTimeout || out != "ok ask A1 raised\nA1 still open → vh ask show A1\n" {
		t.Fatalf("got %d %q", code, out)
	}
	renewals, reads := 0, 0
	for _, p := range paths(f) {
		switch p {
		case "POST /api/v1/leases/L1/renew":
			renewals++
		case "GET /api/v1/asks/A1":
			reads++
		}
	}
	if reads < 2 || renewals != reads {
		t.Errorf("%d renewals for %d reads, want one renewal before each read and several reads", renewals, reads)
	}
}

func TestBlockPollJSONPrintsTheEndedAskAndNoLines(t *testing.T) {
	end := endedAsk("answered", `,"answer":"8080","answered_by":"nik@phone","answered_at":"2026-03-04T12:00:05Z"`)
	f := blockBoard(blockLeaseLive, 200, end)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "block", "aa-1", "Which port?", "--poll", "30s", "--json")
	if code != ExitOK || out != end()+"\n" {
		t.Errorf("got %d %q, want the ended ask only", code, out)
	}
}
