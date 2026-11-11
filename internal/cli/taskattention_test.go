package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// attentionTask is a task document with the reasons and the update time given.
func attentionTask(ref, updated string, reasons ...string) string {
	return fmt.Sprintf(`{"ref":%q,"title":"T %s","status":"open","updated_at":%q,"attention":[%s]}`, ref, ref, updated, strings.Join(reasons, ","))
}

const (
	reasonStranded = `{"reason":"stranded"}`
	reasonAsking   = `{"reason":"asking"}`
	reasonBlocking = `{"reason":"asking","blocking":true,"asker_waiting":true}`
	reasonDone     = `{"reason":"done"}`
)

func TestTaskMarkersNameEachReason(t *testing.T) {
	var doc task
	if err := json.Unmarshal([]byte(attentionTask("aa-1", "", reasonStranded, reasonBlocking, reasonAsking, reasonDone)), &doc); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(doc.markers(), "|"); got != "!stranded|!asking(blocking)|!asking|!done" {
		t.Errorf("markers %q", got)
	}
	if got := taskLine(doc); got != "aa-1  open  T aa-1  !stranded !asking(blocking) !asking !done" {
		t.Errorf("line %q", got)
	}
	if got := taskLine(task{Ref: "aa-2", Status: "open", Title: "x", Blocked: true, Attention: []taskReason{{Reason: "done"}}}); got != "aa-2  open  x  (blocked)  !done" {
		t.Errorf("line with marks %q", got)
	}
	if got := taskLine(task{Ref: "aa-2", Status: "open", Title: "x"}); got != "aa-2  open  x" {
		t.Errorf("line without reasons %q", got)
	}
}

func TestTaskAttentionLineIsEmptyWithoutReasons(t *testing.T) {
	if got := taskAttentionLine(task{}); got != "" {
		t.Errorf("no reasons: %q", got)
	}
	got := taskAttentionLine(task{Attention: []taskReason{{Reason: "stranded"}, {Reason: "asking", Blocking: true}, {Reason: "done"}}})
	if got != "attention: !stranded !asking(blocking) !done\n" {
		t.Errorf("got %q", got)
	}
}

func TestStrandedTextNamesTheLeaseHolderAndAge(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if got := strandedText(task{}, now); got != "" {
		t.Errorf("not stranded: %q", got)
	}
	s := &strandedLease{Ref: "L7", Holder: attentionActor{Handle: "claude@laptop", WayBack: "ssh box"}, Intent: "migrate", ExpiredAt: "2026-05-01T09:00:00Z"}
	want := "stranded: L7 by claude@laptop: \"migrate\" expired 3h ago → board lease ack L7\n"
	if got := strandedText(task{StrandedLease: s}, now); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTaskDetailPrintsAttentionAndStrandedAfterTheInitiative(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	tk := task{Ref: "aa-1", Status: "open", Title: "x", Initiative: "aa", CreatedBy: "a", CreatedAt: "2026-05-01T11:59:30Z",
		Attention:     []taskReason{{Reason: "stranded"}},
		StrandedLease: &strandedLease{Ref: "L1", Holder: attentionActor{Handle: "h"}, Intent: "i", ExpiredAt: "2026-05-01T11:00:00Z"}}
	lines := strings.Split(taskDetail(tk, nil, now), "\n")
	want := []string{"aa-1  open  x  !stranded", "initiative: aa", "attention: !stranded", `stranded: L1 by h: "i" expired 1h ago → board lease ack L1`, "created by a just now"}
	if len(lines) < len(want) || strings.Join(lines[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", lines)
	}
}

func TestOrderAttentionRanksReasonsThenRecency(t *testing.T) {
	docs := []string{
		attentionTask("aa-1", "2026-05-01T10:00:00Z", reasonDone),
		attentionTask("aa-2", "2026-05-01T10:00:00Z", reasonAsking),
		attentionTask("aa-3", "2026-05-01T10:00:00Z", reasonBlocking),
		attentionTask("aa-4", "2026-05-01T09:00:00Z", reasonStranded, reasonDone),
		attentionTask("aa-5", "2026-05-01T11:00:00Z", reasonStranded),
		attentionTask("aa-6", "2026-05-01T12:00:00Z", reasonAsking),
		attentionTask("aa-7", "2026-05-01T12:00:00Z", reasonDone),
		attentionTask("aa-8", "2026-05-01T10:00:00Z", reasonAsking),
		attentionTask("aa-9", "not a time", reasonStranded),
	}
	var raws []json.RawMessage
	for _, d := range docs {
		raws = append(raws, json.RawMessage(d))
	}
	got, err := orderAttention(raws)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, r := range got {
		var tk task
		_ = json.Unmarshal(r, &tk)
		refs = append(refs, tk.Ref)
	}
	if want := "aa-5 aa-4 aa-9 aa-3 aa-6 aa-2 aa-8 aa-7 aa-1"; strings.Join(refs, " ") != want {
		t.Errorf("order %v, want %s", refs, want)
	}
	if _, err := orderAttention([]json.RawMessage{json.RawMessage(`[1]`)}); err == nil {
		t.Error("a document that is not a task is an error")
	}
}

// attentionBoard serves the task list: the attention=true page holds the given documents, the
// attention=false page holds plain tasks aa-10 and aa-11, and every request is kept.
func attentionBoard(attention ...string) *fakeBoard {
	return &fakeBoard{handler: func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("attention") {
		case "true":
			_, _ = fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(attention, ","))
		case "false":
			_, _ = fmt.Fprintf(w, `{"items":[%s,%s]}`, attentionTask("aa-10", "2026-05-01T00:00:00Z"), attentionTask("aa-11", "2026-05-01T00:00:00Z"))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}}
}

func TestTaskLsListsAttentionFirstThenTheRestAndHonoursTheCap(t *testing.T) {
	f := attentionBoard(
		attentionTask("aa-1", "2026-05-01T10:00:00Z", reasonDone),
		attentionTask("aa-2", "2026-05-01T09:00:00Z", reasonStranded),
		attentionTask("aa-3", "2026-05-01T08:00:00Z", reasonBlocking))
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "task", "ls", "--status", "all")
	want := "aa-2  open  T aa-2  !stranded\naa-3  open  T aa-3  !asking(blocking)\naa-1  open  T aa-1  !done\naa-10  open  T aa-10\naa-11  open  T aa-11\n"
	if code != ExitOK || out != want {
		t.Fatalf("got %d %q, want %q", code, out, want)
	}
	var asked []string
	for _, r := range f.requests {
		q := r.URL.Query()
		asked = append(asked, q.Get("attention")+"/"+q.Get("status"))
	}
	if strings.Join(asked, " ") != "true/all false/all" {
		t.Errorf("requests %v", asked)
	}
	_, out, _ = runEnv(f.env(srv), "task", "ls", "--limit", "4")
	if !strings.HasSuffix(out, "aa-10  open  T aa-10\n+1 more → board task ls --all\n") {
		t.Errorf("cap across both groups: %q", out)
	}
	_, out, _ = runEnv(f.env(srv), "task", "ls", "--json", "--limit", "1")
	if strings.Count(out, `"ref"`) != 1 || !strings.Contains(out, `"aa-2"`) {
		t.Errorf("json: %q", out)
	}
}

func TestTaskLsAttentionKeepsOnlyTheTasksThatNeedIt(t *testing.T) {
	f := attentionBoard(
		attentionTask("aa-1", "2026-05-01T10:00:00Z", reasonDone),
		attentionTask("aa-2", "2026-05-01T09:00:00Z", reasonStranded))
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, _ := runEnv(f.env(srv), "task", "ls", "--attention", "--limit", "1")
	if code != ExitOK || out != "aa-2  open  T aa-2  !stranded\n+1 more → board task ls --attention --all\n" {
		t.Fatalf("got %d %q", code, out)
	}
	if len(f.requests) != 1 || f.requests[0].URL.Query().Get("attention") != "true" {
		t.Errorf("--attention asks once, for attention=true: %v", f.requests)
	}
}
