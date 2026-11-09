package cli

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var attentionNow = time.Date(2026, 3, 4, 12, 10, 0, 0, time.UTC)

func TestAttentionActorShowsTheWayBack(t *testing.T) {
	if got := (attentionActor{Handle: "me"}).String(); got != "me" {
		t.Errorf("no way back: got %q", got)
	}
	if got := (attentionActor{Handle: "me", WayBack: "ssh x"}).String(); got != "me (ssh x)" {
		t.Errorf("with a way back: got %q", got)
	}
}

func TestAskAttentionLine(t *testing.T) {
	a := attentionAsk{Ref: "A3", Question: "Which?", Asker: attentionActor{Handle: "me"}, CreatedAt: "2026-03-04T12:00:00Z"}
	if got, want := askAttentionLine(a, attentionNow), `A3  10m ago  board  me  "Which?"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	a.Task, a.Mode, a.AskerWaiting, a.Blocking = "aa-1", "poll", true, true
	if got, want := askAttentionLine(a, attentionNow), `A3  10m ago  aa-1  me  "Which?"  poll  waiting  blocking`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDoneAttentionLine(t *testing.T) {
	d := attentionDone{Task: "aa-1", Title: "T", DoneBy: attentionActor{Handle: "me"}, DoneAt: "2026-03-04T12:00:00Z"}
	if got, want := doneAttentionLine(d, attentionNow), "aa-1  T  done by me 10m ago"; got != want {
		t.Errorf("no lease: got %q, want %q", got, want)
	}
	d.LastLease = &attentionLastLease{Lease: "L2", Outcome: "shipped"}
	if got, want := doneAttentionLine(d, attentionNow), `aa-1  T  done by me 10m ago  L2 "shipped"`; got != want {
		t.Errorf("outcome: got %q, want %q", got, want)
	}
	d.LastLease.Outcome = ""
	if got, want := doneAttentionLine(d, attentionNow), "aa-1  T  done by me 10m ago  L2 ended"; got != want {
		t.Errorf("no outcome: got %q, want %q", got, want)
	}
}

func TestExpiredWatchAndConflictLines(t *testing.T) {
	e := attentionExpired{Lease: "L7", Task: "aa-1", Holder: attentionActor{Handle: "h", WayBack: "w"}, Intent: "go", RenewedAt: "2026-03-04T10:10:00Z"}
	if got, want := expiredAttentionLine(e, attentionNow), `L7  aa-1  h (w)  "go"  renewed 2h ago`; got != want {
		t.Errorf("expired: got %q, want %q", got, want)
	}
	w := attentionWatch{Task: "aa-4", Title: "poll", Owner: attentionActor{Handle: "o"}, NextCheckAt: "2026-03-01T12:10:00Z"}
	if got, want := watchAttentionLine(w, attentionNow), "aa-4  poll  o  due 3d ago"; got != want {
		t.Errorf("watch: got %q, want %q", got, want)
	}
	w.LastCheck = &attentionLastCheck{Summary: "red", By: attentionActor{Handle: "b"}, At: "2026-03-04T12:09:00Z"}
	if got, want := watchAttentionLine(w, attentionNow), `aa-4  poll  o  due 3d ago  last check "red" by b 1m ago`; got != want {
		t.Errorf("watch with a check: got %q, want %q", got, want)
	}
	c := attentionConflict{Resource: "db:x", Holders: []attentionHolder{
		{Lease: "L1", Actor: attentionActor{Handle: "a"}, Task: "aa-1"}, {Lease: "L2", Actor: attentionActor{Handle: "b"}, Task: "aa-2"},
	}}
	if got, want := conflictAttentionLine(c, attentionNow), "CONFLICT db:x: L1 a aa-1, L2 b aa-2"; got != want {
		t.Errorf("conflict: got %q, want %q", got, want)
	}
}

func TestAttentionSectionCap(t *testing.T) {
	items := []string{"a", "b", "c"}
	if got, want := attentionSection("s", items, 2, "board attention"), "s: 3\n  a\n  b\n  +1 more → board attention --all\n"; got != want {
		t.Errorf("capped: got %q, want %q", got, want)
	}
	if got, want := attentionSection("s", items, 0, "x"), "s: 3\n  a\n  b\n  c\n"; got != want {
		t.Errorf("uncapped: got %q, want %q", got, want)
	}
	if got, want := attentionSection("s", items, 3, "x"), "s: 3\n  a\n  b\n  c\n"; got != want {
		t.Errorf("exactly at the cap: got %q, want %q", got, want)
	}
	if got, want := attentionSection("s", nil, 5, "x"), "s: 0\n"; got != want {
		t.Errorf("empty: got %q, want %q", got, want)
	}
}

func TestAttentionTextOrderAndCommand(t *testing.T) {
	got := attentionText(attentionView{}, attentionNow, 5, "board attention")
	heads := strings.Split(strings.TrimSpace(got), "\n")
	if want := "open asks: 0|done, not closed: 0|expired leases: 0|overdue watches: 0|conflicts: 0"; strings.Join(heads, "|") != want {
		t.Errorf("got %q, want %q", strings.Join(heads, "|"), want)
	}
	p, _ := parseArgs([]string{"--tag", "a b", "--tag", "c", "--init", "aa"}, attentionFlags)
	if got, want := attentionCommand(p), `board attention --tag "a b" --tag c --init aa`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAttentionUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"attention", "x"}, {"attention", "--limit", "0"}, {"attention", "--limit", "2", "--all"}, {"attention", "--nope"}} {
		if code, out, errs := runEnv(func(string) string { return "" }, args...); code != ExitUsage || out != "" || errs != AttentionUsage+"\n" {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

func TestAttentionAsksTheServerWithTheFilters(t *testing.T) {
	body := `{"counts":{"open_asks":1},"open_asks":[{"ref":"A1","question":"q","asker":{"handle":"me"},"created_at":"2026-03-04T12:00:00Z"}]}`
	f := answering(200, body)
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "attention", "--tag", "web", "--tag=team", "--init", "aa")
	if code != ExitOK || errs != "" || !strings.HasPrefix(out, "open asks: 1\n  A1  ") || !strings.HasSuffix(out, "conflicts: 0\n") {
		t.Errorf("got %d %q %q", code, out, errs)
	}
	if got := f.requests[0].URL.RawQuery; got != "initiative=aa&tag=web&tag=team" {
		t.Errorf("query %q", got)
	}
	if code, out, _ := runEnv(f.env(srv), "attention", "--json"); code != ExitOK || out != body+"\n" {
		t.Errorf("--json: %d %q", code, out)
	}
}

func TestAttentionRefusalsAndAnUnreadableAnswer(t *testing.T) {
	f := answering(404, `{"title":"x","detail":"there is no initiative zz","next":"GET /api/v1/initiatives"}`)
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "attention", "--init", "zz"); code != ExitNotFound || out != "" || errs != "there is no initiative zz → board init ls\n" {
		t.Errorf("got %d %q %q", code, out, errs)
	}
	bad := answering(200, `[1]`)
	srv2 := httptest.NewServer(bad)
	defer srv2.Close()
	if code, out, errs := runEnv(bad.env(srv2), "attention"); code != ExitError || out != "" || errs == "" {
		t.Errorf("unreadable answer: %d %q %q", code, out, errs)
	}
}
