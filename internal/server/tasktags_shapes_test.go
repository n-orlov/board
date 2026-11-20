package server

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

// marksOfJSON is the "source:tag" words of a decoded tag_marks list.
func marksOfJSON(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("tag_marks = %#v, want a list", v)
	}
	out := []string{}
	for _, m := range list {
		mark := m.(map[string]any)
		out = append(out, mark["source"].(string)+":"+mark["tag"].(string))
	}
	return out
}

func stringsOf(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("tags = %#v, want a list", v)
	}
	out := []string{}
	for _, s := range list {
		out = append(out, s.(string))
	}
	return out
}

func tagBoard(t *testing.T) *keyServer {
	t.Helper()
	s := newKeyServer(t)
	s.create("aa", `,"tags":["web"]`)
	s.create("bb", "")
	s.task(`{"initiative":"aa","title":"flying","tags":["mine"]}`)
	s.task(`{"initiative":"aa","title":"asked about"}`)
	s.task(`{"initiative":"bb","title":"plain","tags":["solo"]}`)
	if rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work"}`, "claude:w1"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
	}
	if rec := s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-2","question":"which?"}`, "codex:c1"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("raise ask = %d %s", rec.Code, rec.Body)
	}
	return s
}

func TestTheNowReadCarriesTheTagsOfEveryTaskItNamesAndNoSecondReadIsNeeded(t *testing.T) {
	t.Parallel()
	s := tagBoard(t)
	rec := s.send(http.MethodGet, "/api/v1/attention/now", "", "claude:w1")
	if rec.Code != http.StatusOK {
		t.Fatalf("now = %d %s", rec.Code, rec.Body)
	}
	doc := decode(t, rec)
	flight := doc["in_flight"].([]any)
	if len(flight) != 1 {
		t.Fatalf("in flight = %v, want aa-1", flight)
	}
	task := flight[0].(map[string]any)["task"].(map[string]any)
	if got := stringsOf(t, task["tags"]); !reflect.DeepEqual(got, []string{"mine"}) {
		t.Errorf("in flight task tags = %v, want its own tag", got)
	}
	if got := marksOfJSON(t, task["tag_marks"]); !reflect.DeepEqual(got, []string{"task:mine", "initiative:web", "actor:claude"}) {
		t.Errorf("in flight task marks = %v", got)
	}
	marks, _ := doc["task_marks"].(map[string]any)
	if len(marks) != 3 || marks["bb-1"] == nil {
		t.Fatalf("task_marks = %v, want aa-1 (in flight), aa-2 (the ask) and bb-1 (recently touched)", marks)
	}
	if got := marksOfJSON(t, marks["bb-1"].(map[string]any)["tag_marks"]); !reflect.DeepEqual(got, []string{"task:solo"}) {
		t.Errorf("bb-1 marks = %v, want its own tag alone", got)
	}
	flying := marks["aa-1"].(map[string]any)
	if !reflect.DeepEqual(marksOfJSON(t, flying["tag_marks"]), marksOfJSON(t, task["tag_marks"])) || !reflect.DeepEqual(stringsOf(t, flying["tags"]), []string{"mine"}) {
		t.Errorf("task_marks of aa-1 = %v, want what the in flight task carries", flying)
	}
	asked := marks["aa-2"].(map[string]any)
	if got := stringsOf(t, asked["tags"]); len(got) != 0 {
		t.Errorf("aa-2 own tags = %v, want none", got)
	}
	if got := marksOfJSON(t, asked["tag_marks"]); !reflect.DeepEqual(got, []string{"initiative:web"}) {
		t.Errorf("aa-2 marks = %v, want the initiative's alone: an ask is not a lease", got)
	}
}

func TestTheNowReadsTaskMarksFollowItsFilters(t *testing.T) {
	t.Parallel()
	s := tagBoard(t)
	rec := s.send(http.MethodGet, "/api/v1/attention/now?initiative=bb", "", "claude:w1")
	doc := decode(t, rec)
	marks, ok := doc["task_marks"].(map[string]any)
	if rec.Code != http.StatusOK || !ok || len(marks) != 1 || marks["bb-1"] == nil {
		t.Fatalf("filtered to bb: %d task_marks = %#v, want bb-1 alone", rec.Code, doc["task_marks"])
	}
	doc = decode(t, s.send(http.MethodGet, "/api/v1/attention/now?tag=claude", "", "claude:w1"))
	if marks := doc["task_marks"].(map[string]any); len(marks) != 1 || marks["aa-1"] == nil {
		t.Errorf("filtered to the claude kind: task_marks = %v, want aa-1 alone", marks)
	}
}

func TestTheTimelineTasksCarryTheirOwnTagsAndSourceMarks(t *testing.T) {
	t.Parallel()
	s := tagBoard(t)
	code, doc := timelineAt(s, url.Values{})
	if code != http.StatusOK {
		t.Fatalf("timeline = %d %v", code, doc)
	}
	byRef := map[string]map[string]any{}
	for _, v := range doc["tasks"].([]any) {
		task := v.(map[string]any)
		byRef[task["ref"].(string)] = task
	}
	want := map[string][2][]string{
		"aa-1": {{"mine"}, {"task:mine", "initiative:web", "actor:claude"}},
		"aa-2": {{}, {"initiative:web"}},
		"bb-1": {{"solo"}, {"task:solo"}},
	}
	for ref, w := range want {
		task := byRef[ref]
		if task == nil {
			t.Fatalf("timeline lacks %s: %v", ref, byRef)
		}
		if got := stringsOf(t, task["tags"]); !reflect.DeepEqual(got, w[0]) {
			t.Errorf("%s tags = %v, want %v", ref, got, w[0])
		}
		if got := marksOfJSON(t, task["tag_marks"]); !reflect.DeepEqual(got, w[1]) {
			t.Errorf("%s marks = %v, want %v", ref, got, w[1])
		}
	}
}
