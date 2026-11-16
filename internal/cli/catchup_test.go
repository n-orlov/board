package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const catchupBoth = `{"notes":[` +
	`{"ref":"N7","created_at":"2026-01-01T11:57:00Z","author":"nik@phone","task":"aa-1","summary":"look at this","has_body":true},` +
	`{"ref":"N8","created_at":"2026-01-01T11:59:00Z","author":"codex@host","summary":"all hands"}],` +
	`"notes_more":0,"answers":[{"ref":"A3","state":"answered","answered_by":"nik@phone","answer":"use the rack AP"}]}`

func atNoon(t *testing.T) {
	t.Helper()
	was := nowFn
	t.Cleanup(func() { nowFn = was })
	nowFn = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }
}

func TestCatchupPostsOnceAndPrintsOneLinePerNoteThenOnePerAnswer(t *testing.T) {
	atNoon(t)
	f := answering(200, catchupBoth)
	code, out, _ := sentBody(t, f, "catchup")
	r := f.requests[0]
	if code != ExitOK || r.Method != "POST" || r.URL.Path != "/api/v1/catchup" || len(f.requests) != 1 {
		t.Fatalf("got %d %s %s (%d requests)", code, r.Method, r.URL.Path, len(f.requests))
	}
	want := "N7  3m ago  nik@phone  aa-1  look at this  +\nN8  1m ago  codex@host  broadcast  all hands\n" +
		"ok A3 answered by nik@phone: use the rack AP\n"
	if out != want {
		t.Errorf("stdout %q, want %q", out, want)
	}
}

func TestCatchupWithNothingNewPrintsNothing(t *testing.T) {
	f := answering(200, `{"notes":[],"notes_more":0,"answers":[]}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "catchup"); code != ExitOK || out != "" || errs != "" {
		t.Errorf("got %d %q %q, want exit 0 and no output", code, out, errs)
	}
}

func TestCatchupSaysHowManyNotesAreLeft(t *testing.T) {
	atNoon(t)
	text := catchupText(catchupView{Notes: []noteView{{Ref: "N1", CreatedAt: "2026-01-01T11:59:00Z", Author: "a", Summary: "s"}}, NotesMore: 3}, nowFn())
	if text != "N1  1m ago  a  broadcast  s\n+3 more → vh catchup\n" {
		t.Errorf("got %q", text)
	}
}

func TestCatchupJSONPrintsTheAnswerAsItCame(t *testing.T) {
	f := answering(200, catchupBoth)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "catchup", "--json"); code != ExitOK || out != catchupBoth+"\n" || errs != "" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
}

func TestCatchupFailuresExitNonZeroWithTheServersWords(t *testing.T) {
	f := answering(http.StatusServiceUnavailable, `{"detail":"the catch-up is not available"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "catchup"); code == ExitOK || out != "" || !strings.Contains(errs, "the catch-up is not available") {
		t.Errorf("got %d %q %q", code, out, errs)
	}
	bad := answering(200, `{"notes": "not an array"}`)
	srv2 := httptest.NewServer(bad)
	defer srv2.Close()
	if code, out, _ := runEnv(bad.env(srv2), "catchup"); code == ExitOK || out != "" {
		t.Errorf("unreadable answer: got %d %q", code, out)
	}
	none := func(string) string { return "" }
	if code, _, _ := runEnv(none, "catchup"); code == ExitOK {
		t.Error("catchup with no server configured succeeded")
	}
}

func TestCatchupMalformedLinesAreUsageErrorsThatSendNothing(t *testing.T) {
	f := answering(200, catchupBoth)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, args := range [][]string{{"catchup", "extra"}, {"catchup", "--no-such"}, {"catchup", "--json=1"}} {
		code, out, errs := runEnv(f.env(srv), args...)
		if code != ExitUsage || out != "" || errs != CatchupUsage+"\n" {
			t.Errorf("%v: got %d %q %q", args, code, out, errs)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("%d requests were sent for malformed command lines", len(f.requests))
	}
}

func TestCatchupIsInTheTreeWithItsFlags(t *testing.T) {
	got, ok := Commands()["catchup"]
	if !ok || strings.Join(got, ",") != "--json" {
		t.Errorf("Commands()[catchup] = %v", got)
	}
}
