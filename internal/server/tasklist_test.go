package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/leases"
)

func TestListTasksAnswers503WithoutAStore(t *testing.T) {
	t.Parallel()
	got, _ := taskOps{}.ListTasks(context.Background(), ListTasksRequestObject{})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("list without a store = %#v, want a 503 problem", got)
	}
}

func TestTaskQueryCarriesEveryParameter(t *testing.T) {
	t.Parallel()
	status, kind := ListTasksParamsStatus("closed"), ListTasksParamsKind("watch")
	ini, tag, win, q, after := []string{"aa", "bb"}, []string{"x"}, "30d", "text", "aa-1"
	yes, limit := true, 5
	got := taskQuery(ListTasksParams{Initiative: &ini, Tag: &tag, Status: &status, Kind: &kind, Active: &yes, Blocked: &yes,
		Overdue: &yes, Attention: &yes, ClosedWithin: &win, Q: &q, Limit: &limit, After: &after}, []string{"aa-2"})
	if fmt.Sprintf("%v %v %s %s %v %v %v %s %s %d %s", got.Initiatives, got.Tags, got.Status, got.Kind, *got.Active, *got.Blocked, *got.Overdue, got.ClosedWithin, got.Q, got.Limit, got.After) !=
		"[aa bb] [x] closed watch true true true 30d text 5 aa-1" {
		t.Errorf("query = %+v", got)
	}
	if !*got.Attention || fmt.Sprint(got.AttentionRefs) != "[aa-2]" {
		t.Errorf("attention filter = %v %v", got.Attention, got.AttentionRefs)
	}
	zero := 0
	if got := taskQuery(ListTasksParams{Limit: &zero}, nil); got.Limit != -1 || got.Status != "" || got.Active != nil {
		t.Errorf("an explicit limit of 0 and nothing else = %+v", got)
	}
}

func TestListTasksShowsLiveLeasesPagesAndRefusesABadCursor(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.task(`{"initiative":"aa","title":"two"}`)
	s.task(`{"initiative":"aa","title":"three"}`)
	ls := &leases.Store{Store: s.st}
	if _, err := ls.Start(context.Background(), audit.Actor{Handle: "codex@host"}, "key:caller", leases.Start{Task: "aa-2", Intent: "wire it"}); err != nil {
		t.Fatal(err)
	}
	rec := s.send(http.MethodGet, "/api/v1/tasks?limit=2", "", "claude@host")
	doc := decode(t, rec)
	items, _ := doc["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 2 || doc["next_after"] == nil {
		t.Fatalf("page = %d %s", rec.Code, rec.Body)
	}
	second, _ := items[1].(map[string]any)
	live, _ := second["live_leases"].([]any)
	if second["ref"] != "aa-2" || second["active"] != true || len(live) != 1 || second["resolution"] != nil {
		t.Errorf("second item = %v", second)
	}
	rest := s.send(http.MethodGet, fmt.Sprintf("/api/v1/tasks?limit=2&after=%v", doc["next_after"]), "", "claude@host")
	if left, _ := decode(t, rest)["items"].([]any); rest.Code != http.StatusOK || len(left) != 1 {
		t.Errorf("last page = %d %s", rest.Code, rest.Body)
	}
	if rec := s.send(http.MethodGet, "/api/v1/tasks?after=aa-9", "", "claude@host"); rec.Code != http.StatusBadRequest {
		t.Errorf("a cursor naming nothing = %d %s", rec.Code, rec.Body)
	}
	if rec := s.send(http.MethodGet, "/api/v1/tasks?initiative=zz", "", "claude@host"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown initiative = %d %s", rec.Code, rec.Body)
	}
	s.send(http.MethodPost, "/api/v1/tasks/aa-3/done", "", "claude@host")
	s.send(http.MethodPost, "/api/v1/tasks/aa-3/close", `{"resolution":"dropped"}`, "claude@host")
	closed := decode(t, s.send(http.MethodGet, "/api/v1/tasks?status=closed", "", "claude@host"))
	if list, _ := closed["items"].([]any); len(list) != 1 || list[0].(map[string]any)["resolution"] != "dropped" {
		t.Errorf("closed list = %v", closed)
	}
}
