package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/timeline"
)

func TestGetTimelineAnswers503WithoutItsStores(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]timelineOps{"no operation": {}, "no leases": {timeline: &timeline.Store{}}, "no timeline": {leases: &leases.Store{}}} {
		got, err := o.GetTimeline(context.Background(), GetTimelineRequestObject{})
		p, ok := got.(problemResponse)
		if err != nil || !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("%s = %#v, %v, want a 503 problem", name, got, err)
		}
	}
}

func timelineAt(s *keyServer, query url.Values) (int, map[string]any) {
	s.t.Helper()
	if query.Get("from") == "" {
		query.Set("from", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339))
		query.Set("to", time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	}
	rec := s.send(http.MethodGet, "/api/v1/timeline?"+query.Encode(), "", "claude@host")
	return rec.Code, decode(s.t, rec)
}

func TestGetTimelineAnswersTheWindowWithAttentionFromTheDerivation(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", `,"tags":["web"]`)
	s.task(`{"initiative":"aa","title":"stranded one"}`)
	s.task(`{"initiative":"aa","title":"quiet one"}`)
	s.task(`{"initiative":"aa","title":"planned one","planned_start":"2026-01-01T00:00:00Z","planned_end":"2026-01-02T00:00:00Z"}`)
	for _, body := range []string{`{"task":"aa-1","intent":"work"}`} {
		if rec := s.send(http.MethodPost, "/api/v1/leases", body, "codex@host"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
		}
	}
	// the lease ran out a minute ago: the task is stranded
	if _, err := s.st.Writer().Exec(s.st.Dialect().Rebind(`UPDATE leases SET expires_at = ?`), store.EncodeTime(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	code, doc := timelineAt(s, url.Values{})
	if code != http.StatusOK {
		t.Fatalf("timeline = %d %v", code, doc)
	}
	tasks, _ := doc["tasks"].([]any)
	if len(tasks) != 3 {
		t.Fatalf("tasks = %v, want aa-1, aa-2 and aa-3 (open, so in the window from its creation)", tasks)
	}
	first, second, third := tasks[0].(map[string]any), tasks[1].(map[string]any), tasks[2].(map[string]any)
	if third["planned_start"] != "2026-01-01T00:00:00Z" || third["planned_end"] != "2026-01-02T00:00:00Z" || third["closed_at"] != nil {
		t.Errorf("planned task = %v", third)
	}
	att, _ := first["attention"].([]any)
	if first["ref"] != "aa-1" || len(att) != 1 || att[0].(map[string]any)["reason"] != "stranded" {
		t.Errorf("stranded task = %v", first)
	}
	if att2, ok := second["attention"].([]any); !ok || len(att2) != 0 || second["ref"] != "aa-2" {
		t.Errorf("quiet task = %v, want an empty attention list", second)
	}
	lease := doc["leases"].([]any)[0].(map[string]any)
	if lease["state"] != "expired" || lease["holder"] != "codex@host" || lease["task"] != "aa-1" || lease["ended_at"] == nil {
		t.Errorf("lease = %v", lease)
	}
	ini := doc["initiatives"].([]any)[0].(map[string]any)
	if ini["slug"] != "aa" || ini["closed"] != false || ini["tags"].([]any)[0] != "web" {
		t.Errorf("initiative = %v", ini)
	}
	if doc["from"] == nil || doc["to"] == nil || doc["markers"] == nil {
		t.Errorf("document = %v", doc)
	}
}

func TestGetTimelineWithoutAnAttentionStoreListsNoReasons(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	got, err := timelineOps{timeline: &timeline.Store{Store: s.st}, leases: &leases.Store{Store: s.st}}.GetTimeline(context.Background(), GetTimelineRequestObject{})
	ok, isOK := got.(GetTimeline200JSONResponse)
	if err != nil || !isOK || len(ok.Tasks) != 1 || ok.Tasks[0].Attention == nil || len(ok.Tasks[0].Attention) != 0 {
		t.Errorf("answer = %#v, %v", got, err)
	}
}

func TestGetTimelineRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	for q, want := range map[string]int{
		"from=yesterday": 400, "to=2000-01-01T00:00:00Z": 400, "from=2030-01-01T00:00:00Z&to=2030-01-01T00:00:00Z": 400,
		"from=2030-01-01T00:00:00Z&to=2035-01-01T00:00:00Z": 400, "closed_within=soon": 400, "rows=some": 400, "rows=ALL": 400, "tag=Bad%20Tag": 400, "initiative=nope": 404,
	} {
		rec := s.send(http.MethodGet, "/api/v1/timeline?"+q, "", "claude@host")
		doc := decode(t, rec)
		if rec.Code != want || doc["next"] == nil || doc["detail"] == nil {
			t.Errorf("%s = %d %s, want %d with a next", q, rec.Code, rec.Body, want)
		}
	}
	rec := s.send(http.MethodGet, "/api/v1/timeline?initiative=nope", "", "claude@host")
	if d := decode(t, rec)["detail"]; d != "there is no initiative nope" {
		t.Errorf("404 detail = %v", d)
	}
	if rec := s.send(http.MethodGet, "/api/v1/timeline?from=x", "", "claude@host"); !strings.Contains(rec.Body.String(), "RFC 3339") {
		t.Errorf("400 body = %s", rec.Body)
	}
}

func TestGetTimelineWithAllRowsKeepsTasksOutsideTheWindow(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"made now"}`)
	past := url.Values{"from": {"2020-01-01T00:00:00Z"}, "to": {"2020-02-01T00:00:00Z"}}
	if code, doc := timelineAt(s, past); code != http.StatusOK || len(doc["tasks"].([]any)) != 0 {
		t.Errorf("the default window = %d %v, want no tasks", code, doc["tasks"])
	}
	past.Set("rows", "window")
	if code, doc := timelineAt(s, past); code != http.StatusOK || len(doc["tasks"].([]any)) != 0 {
		t.Errorf("rows=window = %d %v, want no tasks", code, doc["tasks"])
	}
	past.Set("rows", "all")
	if code, doc := timelineAt(s, past); code != http.StatusOK || len(doc["tasks"].([]any)) != 1 {
		t.Errorf("rows=all = %d %v, want the task", code, doc["tasks"])
	}
}

func TestGetTimelineWithAllRowsAndAnActorKeepsTheTasksTheActorEverLeased(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"leased"}`)
	s.task(`{"initiative":"aa","title":"never leased"}`)
	if rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work"}`, "codex@host"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
	}
	past := url.Values{"from": {"2020-01-01T00:00:00Z"}, "to": {"2020-02-01T00:00:00Z"}, "actor": {"codex@host"}}
	if code, doc := timelineAt(s, past); code != http.StatusOK || len(doc["tasks"].([]any)) != 0 {
		t.Errorf("the window with the actor = %d %v, want no tasks", code, doc["tasks"])
	}
	past.Set("rows", "all")
	code, doc := timelineAt(s, past)
	tasks, _ := doc["tasks"].([]any)
	if code != http.StatusOK || len(tasks) != 1 || tasks[0].(map[string]any)["ref"] != "aa-1" {
		t.Errorf("rows=all with the actor = %d %v, want aa-1 only", code, doc["tasks"])
	}
	if leases, _ := doc["leases"].([]any); len(leases) != 0 {
		t.Errorf("leases = %v, want none: the window still picks the leases", leases)
	}
}

// dueLeaseServer is a server whose one lease ran out a minute ago and is still
// live: an expiry would write it, so a read that expires it is seen. count is the
// lease's state and the number of rows an expiry or any other write would change.
func dueLeaseServer(t *testing.T) (s *keyServer, count func() (state string, n int)) {
	t.Helper()
	s = newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	if rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work"}`, "codex@host"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
	}
	// the lease ran out a minute ago and is still live: an expiry would write it
	if _, err := s.st.Writer().Exec(s.st.Dialect().Rebind(`UPDATE leases SET expires_at = ?`), store.EncodeTime(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	count = func() (state string, n int) {
		row := s.st.Writer().QueryRow(`SELECT (SELECT state FROM leases), (SELECT COUNT(*) FROM events) + (SELECT COUNT(*) FROM tasks) + (SELECT COUNT(*) FROM leases)`)
		if err := row.Scan(&state, &n); err != nil {
			t.Fatal(err)
		}
		return state, n
	}
	if state, _ := count(); state != "live" {
		t.Fatalf("the seeded lease is %q, want live", state)
	}
	return s, count
}

func TestGetTimelineWithABadRowsChangesNothing(t *testing.T) {
	t.Parallel()
	s, count := dueLeaseServer(t)
	_, before := count()
	for q, want := range map[string]int{
		"rows=some": 400, "rows=": 400, "rows=ALL": 400, "rows=window,all": 400, "rows=%20all": 400, "rows=window&rows=some": 400,
		"rows=all&from=yesterday": 400, "rows=all&closed_within=soon": 400, "tag=Bad%20Tag": 400, "initiative=nope": 404, "rows=all&initiative=nope": 404,
	} {
		rec := s.send(http.MethodGet, "/api/v1/timeline?"+q, "", "claude@host")
		if rec.Code != want || decode(t, rec)["next"] == nil {
			t.Errorf("%s = %d %s, want %d with a next", q, rec.Code, rec.Body, want)
		}
		if strings.HasPrefix(q, "rows=") && !strings.Contains(q, "&") && !strings.Contains(rec.Body.String(), "window") {
			t.Errorf("%s = %s, want the values named", q, rec.Body)
		}
		if state, after := count(); state != "live" || after != before {
			t.Errorf("%s left the lease %q and %d rows, want live and %d: a refusal changes nothing", q, state, after, before)
		}
	}
	for _, q := range []string{"", "rows=window", "rows=all"} {
		if rec := s.send(http.MethodGet, "/api/v1/timeline?"+q, "", "claude@host"); rec.Code != http.StatusOK {
			t.Errorf("%q = %d %s, want 200", q, rec.Code, rec.Body)
		}
	}
	if state, _ := count(); state != "expired" {
		t.Errorf("the lease after the accepted reads is %q, want expired by the read", state)
	}
}

// Every other way a timeline query is refused obeys the same rule as a bad rows:
// the request changes nothing, whatever rows it carries.
func TestGetTimelineRefusedWindowsAndFiltersChangeNothing(t *testing.T) {
	t.Parallel()
	s, count := dueLeaseServer(t)
	_, before := count()
	for q, want := range map[string]int{
		"to=2000-01-01T00:00:00Z":                                                   400, // ends before the default window starts
		"from=2030-01-01T00:00:00Z&to=2030-01-01T00:00:00Z&rows=all":                400, // empty window
		"from=2030-01-01T00:00:00Z&to=2035-01-01T00:00:00Z&rows=window":             400, // over 400 days
		"from=2030-01-01T00:00:00Z&to=later":                                        400,
		"closed_within=soon&rows=window":                                            400,
		"tag=Bad%20Tag&rows=all":                                                    400,
		"initiative=nope&rows=window":                                               404,
		"initiative=nope&rows=some":                                                 400, // rows is judged first
		"rows=some&initiative=nope&tag=Bad%20Tag&closed_within=soon&from=yesterday": 400,
		"active=true&window=2h":                                                     400, // a window that is none of the three
		"window=1d":                                                                 400, // judged whether or not active is asked for
	} {
		rec := s.send(http.MethodGet, "/api/v1/timeline?"+q, "", "claude@host")
		if rec.Code != want || decode(t, rec)["next"] == nil {
			t.Errorf("%s = %d %s, want %d with a next", q, rec.Code, rec.Body, want)
		}
		if state, after := count(); state != "live" || after != before {
			t.Errorf("%s left the lease %q and %d rows, want live and %d: a refusal changes nothing", q, state, after, before)
		}
	}
}

// A read that is accepted keeps expiring the due leases, for each valid rows and
// for rows left out: only a refusal is exempt from the write.
func TestGetTimelineAcceptedRowsStillExpireTheDueLeases(t *testing.T) {
	t.Parallel()
	for _, q := range []string{"", "rows=window", "rows=all"} {
		s, count := dueLeaseServer(t)
		if rec := s.send(http.MethodGet, "/api/v1/timeline?"+q, "", "claude@host"); rec.Code != http.StatusOK {
			t.Fatalf("%q = %d %s, want 200", q, rec.Code, rec.Body)
		}
		if state, _ := count(); state != "expired" {
			t.Errorf("%q left the lease %q, want expired by the read", q, state)
		}
	}
}

func TestTimelineRowsAcceptOnlyTheDeclaredValues(t *testing.T) {
	t.Parallel()
	for _, v := range []GetTimelineParamsRows{GetTimelineParamsRowsWindow, GetTimelineParamsRowsAll} {
		if !v.Valid() {
			t.Errorf("%v is declared but reported invalid", v)
		}
	}
	if GetTimelineParamsRows("some").Valid() {
		t.Error("an undeclared rows value is valid")
	}
	if got := rowsText(nil); got != "" {
		t.Errorf("no rows = %q, want empty", got)
	}
	all := GetTimelineParamsRowsAll
	if got := rowsText(&all); got != "all" {
		t.Errorf("rows all = %q", got)
	}
}

// active=true keeps a task that carries an attention reason though nothing touched it
// in the window: the derivation's refs reach the store's query.
func TestGetTimelineActiveKeepsTheTasksWithAnAttentionReason(t *testing.T) {
	t.Parallel()
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"stranded one"}`)
	s.task(`{"initiative":"aa","title":"quiet one"}`)
	if rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work"}`, "codex@host"); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("start lease = %d %s", rec.Code, rec.Body)
	}
	if _, err := s.st.Writer().Exec(s.st.Dialect().Rebind(`UPDATE leases SET expires_at = ?`), store.EncodeTime(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	// nothing was touched in the last 3 days
	if _, err := s.st.Writer().Exec(s.st.Dialect().Rebind(`UPDATE events SET at = ?`), store.EncodeTime(time.Now().Add(-100*time.Hour))); err != nil {
		t.Fatal(err)
	}
	refs := func(query string) []string {
		code, doc := timelineAt(s, mustValues(t, query))
		if code != http.StatusOK {
			t.Fatalf("%s = %d %v", query, code, doc)
		}
		var out []string
		for _, task := range doc["tasks"].([]any) {
			out = append(out, task.(map[string]any)["ref"].(string))
		}
		return out
	}
	if got := refs("active=true&window=3d"); len(got) != 1 || got[0] != "aa-1" {
		t.Errorf("active tasks = %v, want only the stranded aa-1", got)
	}
	if got := refs("active=false"); len(got) != 2 {
		t.Errorf("without active = %v, want both tasks", got)
	}
}

func mustValues(t *testing.T, query string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
