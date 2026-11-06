package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/tasks"
)

func (s *keyServer) task(body string) {
	s.t.Helper()
	rec := s.send(http.MethodPost, "/api/v1/tasks", body, "claude@host")
	if rec.Code != http.StatusCreated {
		s.t.Fatalf("create task %s = %d %s", body, rec.Code, rec.Body)
	}
}

func TestGetTaskShowsItsLiveLeasesAndIsActiveOnlyWhileOneIsLive(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.task(`{"initiative":"aa","title":"two"}`)
	ls := &leases.Store{Store: s.st}
	started, err := ls.Start(context.Background(), audit.Actor{Handle: "codex@host"}, "key:caller",
		leases.Start{Task: "aa-1", Intent: "wire it", Resources: []string{"repo:board", "file:a"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-1", "", "claude@host"))
	if doc["active"] != true || doc["blocked"] != false || doc["overdue"] != false {
		t.Errorf("derived state = %v %v %v", doc["active"], doc["blocked"], doc["overdue"])
	}
	list, _ := doc["live_leases"].([]any)
	if len(list) != 1 {
		t.Fatalf("live_leases = %v", doc["live_leases"])
	}
	l, _ := list[0].(map[string]any)
	if l["ref"] != started.Lease.Ref || l["task"] != "aa-1" || l["actor"] != "codex@host" || l["intent"] != "wire it" ||
		l["state"] != "live" || fmt.Sprint(l["resources"]) != "[file:a repo:board]" || l["ttl_seconds"] != float64(7200) {
		t.Errorf("lease = %v", l)
	}
	for _, absent := range []string{"ended_at", "ended_by", "outcome", "acked_at", "acked_by"} {
		if _, has := l[absent]; has {
			t.Errorf("a live lease carries %s", absent)
		}
	}
	other := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-2", "", "claude@host"))
	if other["active"] != false || fmt.Sprint(other["live_leases"]) != "[]" {
		t.Errorf("aa-2 active %v leases %v", other["active"], other["live_leases"])
	}
	if _, err := ls.End(context.Background(), audit.Actor{Handle: "codex@host"}, "key:caller", started.Lease.Ref, ""); err != nil {
		t.Fatal(err)
	}
	again := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-1", "", "claude@host"))
	if again["active"] != false || fmt.Sprint(again["live_leases"]) != "[]" {
		t.Errorf("after the end: active %v leases %v", again["active"], again["live_leases"])
	}
}

func TestGetTaskCarriesOverdueBlockedAndClosedFields(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"watch","kind":"watch","cadence_seconds":60,"next_check_at":"2020-01-01T00:00:00Z"}`)
	s.task(`{"initiative":"aa","title":"waits","blocked_by":["aa-1"]}`)
	watch := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-1", "", "a"))
	if watch["overdue"] != true || watch["blocked"] != false {
		t.Errorf("watch = %v", watch)
	}
	if blocking, _ := watch["blocking"].([]any); len(blocking) != 1 {
		t.Errorf("blocking = %v", watch["blocking"])
	}
	waits := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-2", "", "a"))
	if waits["blocked"] != true || waits["overdue"] != false {
		t.Errorf("waits = %v", waits)
	}
	for _, absent := range []string{"resolution", "done_at", "done_by", "closed_at", "closed_by", "planned_start", "cadence_seconds"} {
		if _, has := waits[absent]; has {
			t.Errorf("an open work task carries %s", absent)
		}
	}
}

func TestTasksAreUnavailableWithoutAStore(t *testing.T) {
	h := NewHandler(ok, acceptKey, nil, nil)
	for _, c := range []struct{ method, path string }{{"POST", "/api/v1/tasks"}, {"GET", "/api/v1/tasks/aa-1"}, {"PATCH", "/api/v1/tasks/aa-1"}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authedWith(c.method, c.path, `{"initiative":"aa","title":"t"}`))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d", c.method, c.path, rec.Code)
		}
	}
}

func TestTaskCreateNeedsAUsableBody(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/tasks", `null`}, {"POST", "/api/v1/tasks", `{"initiative":"aa","title":`},
	} {
		rec := s.send(c.method, c.path, c.body, "a")
		doc := decode(t, rec)
		if rec.Code != http.StatusBadRequest || doc["next"] == nil || doc["next"] == "" {
			t.Errorf("%s %s %s = %d %v", c.method, c.path, c.body, rec.Code, doc)
		}
	}
}

func TestMaybeTellsAbsentNullAndAValueApart(t *testing.T) {
	var patch TaskPatch
	if err := json.Unmarshal([]byte(`{"title":"t","planned_start":null}`), &patch); err != nil {
		t.Fatal(err)
	}
	if !patch.Title.Set || patch.Title.Null || patch.Title.Value != "t" || !patch.PlannedStart.Set || !patch.PlannedStart.Null || patch.PlannedEnd.Set {
		t.Errorf("patch = %+v", patch)
	}
	if b, _ := (Maybe[int64]{Set: true, Value: 5}).MarshalJSON(); string(b) != "5" {
		t.Errorf("value marshals as %s", b)
	}
	if b, _ := (Maybe[int64]{}).MarshalJSON(); strings.TrimSpace(string(b)) != "null" {
		t.Errorf("unset marshals as %s", b)
	}
	if b, _ := (Maybe[int64]{Set: true, Null: true}).MarshalJSON(); string(b) != "null" {
		t.Errorf("null marshals as %s", b)
	}
}

func TestPatchTaskAnswersEachRefusalWithAProblemAndNext(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.task(`{"initiative":"aa","title":"two"}`)
	if _, err := s.st.Writer().Exec(`UPDATE tasks SET status = 'closed' WHERE seq = 2`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, path, body string
		want             int
		next             string
	}{
		{"an immutable kind", "aa-1", `{"kind":"work"}`, 400, "Leave kind"},
		{"an immutable initiative", "aa-1", `{"initiative":"aa"}`, 400, "Leave kind"},
		{"a null title", "aa-1", `{"title":null}`, 400, "title"},
		{"a closed task", "aa-2", `{"title":"x"}`, 409, "/api/v1/tasks/aa-2/reopen"},
		{"an unknown task", "aa-9", `{"title":"x"}`, 404, "GET /api/v1/tasks"},
		{"a malformed body", "aa-1", `{"title":`, 400, ""},
	} {
		rec := s.send(http.MethodPatch, "/api/v1/tasks/"+c.path, c.body, "claude@host")
		doc := decode(t, rec)
		if rec.Code != c.want || rec.Header().Get("Content-Type") != "application/problem+json" ||
			!strings.Contains(fmt.Sprint(doc["next"]), c.next) || doc["detail"] == nil {
			t.Errorf("%s: %d %v", c.name, rec.Code, doc)
		}
	}
	if n := len(s.events()); n != 3 {
		t.Errorf("events = %d, want the initiative's and two creations", n)
	}
}

func TestTaskFailureMapsEachStoreErrorToItsStatus(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		detail string
	}{
		{tasks.InvalidError{Msg: "bad title", Next: "Fix it."}, 400, "bad title"},
		{fmt.Errorf("%w: zz", tasks.ErrNoInitiative), 404, "initiative zz"},
		{fmt.Errorf("%w: aa-3", tasks.ErrNotFound), 404, "missing aa-3"},
		{fmt.Errorf("%w: zz", tasks.ErrInitiativeClosed), 409, "initiative zz is closed"},
		{fmt.Errorf("%w: aa-3", tasks.ErrClosed), 409, "task aa-3 is closed"},
	} {
		p, ok := taskFailure(c.err, "missing ")
		if !ok || p.status != c.status || !strings.Contains(p.detail, c.detail) || p.next == "" {
			t.Errorf("%v: %+v %v", c.err, p, ok)
		}
	}
	if _, ok := taskFailure(errors.New("disk"), "x"); ok {
		t.Error("an error of the server's own was mapped to a problem")
	}
	if _, err := taskResult[GetTaskResponseObject](errors.New("disk"), "x"); err == nil {
		t.Error("taskResult hid an error of the server's own")
	}
}

func TestCreateTaskInAClosedInitiativeIs409AndAnUnknownOneIs404(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	if _, err := s.st.Writer().Exec(`UPDATE initiatives SET status = 'closed'`); err != nil {
		t.Fatal(err)
	}
	if rec := s.send(http.MethodPost, "/api/v1/tasks", `{"initiative":"aa","title":"t"}`, "a"); rec.Code != 409 {
		t.Errorf("closed initiative = %d %s", rec.Code, rec.Body)
	}
	if rec := s.send(http.MethodPost, "/api/v1/tasks", `{"initiative":"zz","title":"t"}`, "a"); rec.Code != 404 {
		t.Errorf("unknown initiative = %d %s", rec.Code, rec.Body)
	}
}

func TestPatchTaskReturnsTheEditedTaskAndOnlyAChangeWritesAnEvent(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	rec := s.send(http.MethodPatch, "/api/v1/tasks/AA-1", `{"title":"two","planned_start":"2026-12-01T10:00:00Z"}`, "codex@host")
	doc := decode(t, rec)
	if rec.Code != 200 || doc["title"] != "two" || doc["planned_start"] != "2026-12-01T10:00:00Z" || doc["ref"] != "aa-1" {
		t.Fatalf("patch = %d %v", rec.Code, doc)
	}
	ev := s.events()
	last := ev[len(ev)-1]
	if last.typ != "task_updated" || last.actor != "codex@host" || last.principal != "key:caller" ||
		!strings.Contains(last.data, `"changed":["title","planned_start"]`) {
		t.Errorf("event = %+v", last)
	}
	if rec := s.send(http.MethodPatch, "/api/v1/tasks/aa-1", `{"title":"two"}`, "other@host"); rec.Code != 200 || len(s.events()) != len(ev) {
		t.Errorf("a no-op patch = %d, events %d", rec.Code, len(s.events()))
	}
}

func TestTaskEnumsAcceptOnlyTheDeclaredValues(t *testing.T) {
	for name, c := range map[string]struct {
		valid   []interface{ Valid() bool }
		invalid interface{ Valid() bool }
	}{
		"kind": {[]interface{ Valid() bool }{TaskKindWork, TaskKindWatch, TaskCreateKindWork, TaskCreateKindWatch, TaskDetailKindWork, TaskDetailKindWatch}, TaskKind("other")},
		"status": {[]interface{ Valid() bool }{TaskStatusOpen, TaskStatusDone, TaskStatusClosed, TaskDetailStatusOpen, TaskDetailStatusDone, TaskDetailStatusClosed,
			TaskLinkStatusOpen, TaskLinkStatusDone, TaskLinkStatusClosed}, TaskStatus("gone")},
		"resolution": {[]interface{ Valid() bool }{TaskResolutionCompleted, TaskResolutionDropped, TaskResolutionSuperseded, TaskResolutionDuplicate,
			TaskDetailResolutionCompleted, TaskDetailResolutionDropped, TaskDetailResolutionSuperseded, TaskDetailResolutionDuplicate}, TaskResolution("x")},
		"lease state": {[]interface{ Valid() bool }{LeaseStateLive, LeaseStateEnded, LeaseStateExpired}, LeaseState("x")},
	} {
		for _, v := range c.valid {
			if !v.Valid() {
				t.Errorf("%s: %v is declared but reported invalid", name, v)
			}
		}
		if c.invalid.Valid() {
			t.Errorf("%s: an undeclared value is valid", name)
		}
	}
	for _, v := range []interface{ Valid() bool }{TaskCreateKind("x"), TaskDetailKind("x"), TaskDetailStatus("x"), TaskLinkStatus("x"), TaskDetailResolution("x")} {
		if v.Valid() {
			t.Errorf("%T: an undeclared value is valid", v)
		}
	}
}
