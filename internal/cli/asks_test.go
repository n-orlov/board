package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const askAnswer = `{"id":"01A","ref":"A1","task":"aa-1","asker":"me","question":"Which port?","details":"","options":[],"mode":"stop","blocking":false,"state":"open","created_at":"2026-03-04T12:00:00Z","asker_waiting":false}`

func TestAskLineByState(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 10, 0, 0, time.UTC)
	base := askView{Ref: "A7", Task: "aa-1", Asker: "me", Question: `say "hi"`, CreatedAt: "2026-03-04T12:00:00Z"}
	for _, c := range []struct {
		name string
		edit func(*askView)
		want string
	}{
		{"open", func(a *askView) { a.State = "open" }, `A7  open  aa-1  me  "say \"hi\""  asked 10m ago`},
		{"blocking poll", func(a *askView) { a.State, a.Blocking, a.Mode = "open", true, "poll" }, `A7  open  aa-1  me  "say \"hi\""  (blocking, poll)  asked 10m ago`},
		{"poll only", func(a *askView) { a.State, a.Mode = "open", "poll" }, `A7  open  aa-1  me  "say \"hi\""  (poll)  asked 10m ago`},
		{"no task", func(a *askView) { a.State, a.Task = "open", "" }, `A7  open  -  me  "say \"hi\""  asked 10m ago`},
		{"answered", func(a *askView) { a.State, a.AnsweredBy, a.AnsweredAt = "answered", "nik", "2026-03-04T12:07:00Z" }, `A7  answered  aa-1  me  "say \"hi\""  answered by nik 3m ago`},
		{"withdrawn", func(a *askView) { a.State, a.WithdrawnAt = "withdrawn", "2026-03-04T12:08:00Z" }, `A7  withdrawn  aa-1  me  "say \"hi\""  withdrawn 2m ago`},
	} {
		a := base
		c.edit(&a)
		if got := askLine(a, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAskDetailListsFactsOptionsDetailsAndOutcome(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 10, 0, 0, time.UTC)
	a := askView{Ref: "A1", Task: "aa-1", Initiative: "aa", Asker: "me", Question: "q", State: "open", CreatedAt: "2026-03-04T12:00:00Z", Options: []string{"x", "y"}, Details: "more text"}
	want := "A1  open  aa-1  me  \"q\"  asked 10m ago\ninitiative: aa\noption: x\noption: y\nmore text\n"
	if got := askDetail(a, now); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := askOutcome(askView{State: "answered", Answer: "8080", Channel: "relay", OnBehalfOf: "nik"}); got != "answer (relay for nik): 8080\n" {
		t.Errorf("got %q", got)
	}
	if got := askOutcome(askView{State: "answered", Answer: "8080", Channel: "api"}); got != "answer (api): 8080\n" {
		t.Errorf("got %q", got)
	}
	if got := askOutcome(askView{State: "withdrawn", WithdrawReason: "moot"}); got != "reason: moot\n" {
		t.Errorf("got %q", got)
	}
	if got := askOutcome(askView{State: "withdrawn"}); got != "" {
		t.Errorf("got %q", got)
	}
	if got := askOutcome(askView{State: "open"}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAskRaiseSendsTheQuestionTheTaskAndTheFlagsGiven(t *testing.T) {
	details := filepath.Join(t.TempDir(), "details.txt")
	if err := os.WriteFile(details, []byte("the details"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := answering(201, askAnswer)
	code, out, body := sentBody(t, f, "ask", "AA-1", "Which port?", "--details", details, "--option", "8080", "--option=9090", "--blocking", "--stop")
	r := f.requests[0]
	if code != ExitOK || out != "ok ask A1 raised\n" || r.Method != "POST" || r.URL.Path != "/api/v1/asks" {
		t.Fatalf("got %d %q %s %s", code, out, r.Method, r.URL.Path)
	}
	opts, _ := body["options"].([]any)
	if body["task"] != "AA-1" || body["question"] != "Which port?" || body["mode"] != "stop" || body["details"] != "the details" || body["blocking"] != true || len(opts) != 2 {
		t.Errorf("body %v", body)
	}
}

func TestAskRaiseSendsNoDetailsOptionsOrBlockingByDefault(t *testing.T) {
	_, _, body := sentBody(t, answering(201, askAnswer), "ask", "aa-1", "q")
	for _, k := range []string{"details", "options", "blocking"} {
		if _, ok := body[k]; ok {
			t.Errorf("body %v carries %s", body, k)
		}
	}
}

func TestAskDetailsFromStandardInputAndAnUnreadableFile(t *testing.T) {
	old := stdin
	defer func() { stdin = old }()
	stdin = strings.NewReader("piped")
	_, _, body := sentBody(t, answering(201, askAnswer), "ask", "aa-1", "q", "--details", "-")
	if body["details"] != "piped" {
		t.Errorf("body %v", body)
	}
	f := answering(201, askAnswer)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "ask", "aa-1", "q", "--details", "/nonexistent/x")
	if code != ExitError || out != "" || errs != "cannot read the details file /nonexistent/x\n" || len(f.requests) != 0 {
		t.Errorf("got %d %q %q with %d requests", code, out, errs, len(f.requests))
	}
}

func TestAskRefusalsWordTheirNext(t *testing.T) {
	for _, c := range []struct {
		args   []string
		status int
		code   int
		want   string
	}{
		{[]string{"ask", "AA-1", "q"}, http.StatusNotFound, ExitNotFound, "no such → board task ls\n"},
		{[]string{"ask", "AA-1", "q"}, http.StatusConflict, ExitConflict, "no such → board task show aa-1\n"},
		{[]string{"ask", "AA-1", "q"}, http.StatusBadRequest, ExitError, "no such\n"},
		{[]string{"ask", "show", "a9"}, http.StatusNotFound, ExitNotFound, "no such → board ask ls\n"},
		{[]string{"answer", "a9", "x"}, http.StatusNotFound, ExitNotFound, "no such → board ask ls\n"},
		{[]string{"answer", "a9", "x"}, http.StatusConflict, ExitConflict, "no such → board ask show A9\n"},
		{[]string{"ask", "withdraw", "a9"}, http.StatusConflict, ExitConflict, "no such → board ask show A9\n"},
		{[]string{"ask", "withdraw", "a9"}, http.StatusBadRequest, ExitError, "no such\n"},
		{[]string{"ask", "ls", "--task", "zz-1"}, http.StatusNotFound, ExitNotFound, "no such → board task ls\n"},
	} {
		f := answering(c.status, `{"detail":"no such"}`)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), c.args...)
		srv.Close()
		if code != c.code || out != "" || errs != c.want {
			t.Errorf("%v %d: got %d %q %q", c.args, c.status, code, out, errs)
		}
	}
}

func TestAnswerSendsTheAnswerAndTheRelayFlags(t *testing.T) {
	f := answering(200, strings.Replace(askAnswer, `"state":"open"`, `"state":"answered"`, 1))
	code, out, body := sentBody(t, f, "answer", "a1", "8080")
	r := f.requests[0]
	if code != ExitOK || out != "ok ask A1 answered\n" || r.Method != "POST" || r.URL.Path != "/api/v1/asks/a1/answer" {
		t.Fatalf("got %d %q %s %s", code, out, r.Method, r.URL.Path)
	}
	if len(body) != 1 || body["answer"] != "8080" {
		t.Errorf("body %v", body)
	}
	_, _, body = sentBody(t, answering(200, askAnswer), "answer", "--relay", "A1", "8080", "--for", "nik")
	if body["channel"] != "relay" || body["on_behalf_of"] != "nik" || body["answer"] != "8080" {
		t.Errorf("body %v", body)
	}
	_, _, body = sentBody(t, answering(200, askAnswer), "answer", "A1", "8080", "--relay")
	if body["channel"] != "relay" || body["on_behalf_of"] != nil {
		t.Errorf("body %v", body)
	}
}

func TestAskWithdrawSendsTheReasonOnlyWhenGiven(t *testing.T) {
	f := answering(200, askAnswer)
	code, out, body := sentBody(t, f, "ask", "withdraw", "A1", "moot")
	if code != ExitOK || out != "ok ask A1 withdrawn\n" || f.requests[0].URL.Path != "/api/v1/asks/A1/withdraw" || body["reason"] != "moot" {
		t.Errorf("got %d %q %v", code, out, body)
	}
	f = answering(200, askAnswer)
	_, _, body = sentBody(t, f, "ask", "withdraw", "A1")
	if len(f.bodies[0]) != 0 || body != nil {
		t.Errorf("body %q", f.bodies[0])
	}
}

func TestAskWritesPrintTheAnswerWithJSON(t *testing.T) {
	for _, args := range [][]string{
		{"ask", "aa-1", "q", "--json"}, {"ask", "withdraw", "A1", "--json"}, {"answer", "A1", "x", "--json"}, {"ask", "show", "A1", "--json"},
	} {
		f := answering(200, askAnswer)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), args...)
		srv.Close()
		if code != ExitOK || out != askAnswer+"\n" || errs != "" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

func TestAskUsageErrors(t *testing.T) {
	for _, c := range []struct {
		args  []string
		usage string
	}{
		{[]string{"ask"}, AskUsage}, {[]string{"ask", "aa-1"}, AskUsage}, {[]string{"ask", "aa-1", "q", "x"}, AskUsage},
		{[]string{"ask", "aa-1", "q", "--poll", "5m"}, AskUsage}, {[]string{"ask", "aa-1", "q", "--option"}, AskUsage},
		{[]string{"ask", "show"}, AskUsage}, {[]string{"ask", "show", "A1", "A2"}, AskUsage},
		{[]string{"ask", "withdraw"}, AskUsage}, {[]string{"ask", "withdraw", "A1", "r", "x"}, AskUsage},
		{[]string{"ask", "ls", "x"}, AskUsage}, {[]string{"ask", "ls", "--state", "pending"}, AskUsage},
		{[]string{"ask", "ls", "--limit", "0"}, AskUsage}, {[]string{"ask", "ls", "--all", "--limit", "3"}, AskUsage},
		{[]string{"answer"}, AnswerUsage}, {[]string{"answer", "A1"}, AnswerUsage}, {[]string{"answer", "A1", "x", "y"}, AnswerUsage},
		{[]string{"answer", "A1", "x", "--for", "nik"}, AnswerUsage}, {[]string{"answer", "A1", "x", "--bogus"}, AnswerUsage},
	} {
		f := answering(200, askAnswer)
		srv := httptest.NewServer(f)
		code, out, errs := runEnv(f.env(srv), c.args...)
		srv.Close()
		if code != ExitUsage || out != "" || errs != c.usage+"\n" || len(f.requests) != 0 {
			t.Errorf("%v: got %d %q %q with %d requests", c.args, code, out, errs, len(f.requests))
		}
	}
}

func TestAskLsSendsTheFiltersAndCapsTheList(t *testing.T) {
	page := func(from, to int) string {
		items := make([]string, 0, to-from+1)
		for i := from; i <= to; i++ {
			items = append(items, strings.Replace(askAnswer, `"ref":"A1"`, fmt.Sprintf(`"ref":"A%d"`, i), 1))
		}
		return `{"items":[` + strings.Join(items, ",") + `]}`
	}
	f := answering(200, page(1, 3))
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "ask", "ls", "--mine", "--state", "all", "--task", "aa-1", "--limit", "2")
	q := f.requests[0].URL.Query()
	if code != ExitOK || errs != "" || q.Get("actor") != "claude:test" || q.Get("state") != "all" || q.Get("task") != "aa-1" {
		t.Fatalf("got %d %q %q query %v", code, out, errs, q)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "A1  open  aa-1  me  ") || lines[2] != "+1 more → board ask ls --state all --mine --task aa-1 --all" {
		t.Errorf("got %q", out)
	}
	f2 := answering(200, page(1, 2))
	srv2 := httptest.NewServer(f2)
	defer srv2.Close()
	_, _, _ = runEnv(f2.env(srv2), "ask", "ls")
	if q := f2.requests[0].URL.Query(); q.Get("actor") != "" || q.Get("state") != "" || q.Get("task") != "" {
		t.Errorf("a plain ls sent filters: %v", q)
	}
}

func TestNoAskFlagIsKeyLike(t *testing.T) {
	for _, specs := range [][]flagSpec{askFlags, askShowFlags, askLsFlags, askDropFlags, answerFlags} {
		for _, s := range specs {
			for _, banned := range []string{"key", "token", "secret", "password", "bearer", "auth"} {
				if strings.Contains(strings.ToLower(s.Name), banned) {
					t.Errorf("%s: the API key is environment-only", s.Name)
				}
			}
		}
	}
}
