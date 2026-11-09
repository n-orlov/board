package notes

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/tasks"
)

var (
	t0  = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	ctx = context.Background()
	me  = audit.Actor{Handle: "claude@host"}
	bob = audit.Actor{Handle: "codex@host"}
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type env struct {
	S     Store
	Clock *clock
}

// newEnv is a migrated SQLite database with initiatives aa and bb, tasks aa-1,
// aa-2 and bb-1, and a moving clock.
func newEnv(t *testing.T) env {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := &clock{now: t0}
	ini := initiatives.Store{Store: st, Now: c.Now}
	for _, slug := range []string{"aa", "bb"} {
		if _, err := ini.Create(ctx, me, "key:x", initiatives.New{Slug: slug, Title: "I"}); err != nil {
			t.Fatal(err)
		}
	}
	ts := tasks.Store{Store: st, Now: c.Now}
	for _, in := range []string{"aa", "aa", "bb"} {
		c.now = c.now.Add(time.Second)
		if _, err := ts.Create(ctx, me, "key:x", tasks.New{Initiative: in, Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	return env{S: Store{Store: st, Now: c.Now}, Clock: c}
}

// post posts a note as me, one second after the last call.
func (e env) post(t *testing.T, in New) Note {
	t.Helper()
	return e.postAs(t, me, in)
}

func (e env) postAs(t *testing.T, a audit.Actor, in New) Note {
	t.Helper()
	e.Clock.now = e.Clock.now.Add(time.Second)
	k, err := e.S.Post(ctx, a, "key:x", in)
	if err != nil {
		t.Fatalf("post %+v: %v", in, err)
	}
	return k
}

// counts is the number of notes, events and actors rows.
func (e env) counts(t *testing.T) [3]int {
	t.Helper()
	var out [3]int
	for i, table := range []string{"notes", "events", "actors"} {
		if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

type event struct {
	Typ, Principal, Summary, RefID string
	Task, Initiative               *string
	Data                           map[string]any
}

func (e env) lastEvent(t *testing.T) event {
	t.Helper()
	var ev event
	var data string
	err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT type, principal, summary, ref_id, task_id, initiative_id, data
		FROM events ORDER BY n DESC LIMIT 1`).Scan(&ev.Typ, &ev.Principal, &ev.Summary, &ev.RefID, &ev.Task, &ev.Initiative, &data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(data), &ev.Data); err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestPostWritesNotePostedInTheSameTransactionForTheFourKinds(t *testing.T) {
	e := newEnv(t)
	task, _ := e.S.findTask(ctx, e.S.Store.Reader(), "aa-1")
	ini, _ := (initiatives.Store{Store: e.S.Store}).Get(ctx, "aa")
	for _, tc := range []struct {
		name           string
		in             New
		task, ini      string
		broadcast      bool
		summary        string
		dataKey, dataV string
	}{
		{"task", New{Task: "aa-1", Summary: "on task"}, task.ID, ini.ID, false, "note N1 on aa-1: on task", "task", "aa-1"},
		{"initiative", New{Initiative: "aa", Summary: "on initiative"}, "", ini.ID, false, "note N2 on aa: on initiative", "initiative", "aa"},
		{"resource", New{Resource: "host:gw1", Summary: "on  a   resource"}, "", "", true, "note N3 on host:gw1: on a resource", "resource", "host:gw1"},
		{"broadcast", New{Summary: "to all"}, "", "", true, "note N4 (broadcast): to all", "broadcast", ""},
	} {
		before := e.counts(t)
		k := e.post(t, tc.in)
		after := e.counts(t)
		if after[0] != before[0]+1 || after[1] != before[1]+1 {
			t.Errorf("%s: notes/events went %v -> %v, want one of each", tc.name, before, after)
		}
		ev := e.lastEvent(t)
		if ev.Typ != EventPosted || ev.RefID != k.ID || ev.Principal != "key:x" || ev.Summary != tc.summary {
			t.Errorf("%s: event = %+v, want type %s ref %s summary %q", tc.name, ev, EventPosted, k.ID, tc.summary)
		}
		if got := deref(ev.Task); got != tc.task {
			t.Errorf("%s: event task = %q, want %q", tc.name, got, tc.task)
		}
		if got := deref(ev.Initiative); got != tc.ini {
			t.Errorf("%s: event initiative = %q, want %q", tc.name, got, tc.ini)
		}
		if k.Broadcast != tc.broadcast || ev.Data["broadcast"] != tc.broadcast || ev.Data["ref"] != k.Ref {
			t.Errorf("%s: broadcast = %v, data = %v", tc.name, k.Broadcast, ev.Data)
		}
		if _, hasBody := ev.Data["body"]; hasBody {
			t.Errorf("%s: event data carries the body", tc.name)
		}
		if tc.dataV != "" && ev.Data[tc.dataKey] != tc.dataV {
			t.Errorf("%s: data[%s] = %v, want %s", tc.name, tc.dataKey, ev.Data[tc.dataKey], tc.dataV)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TestABroadcastNoteWritesNotePosted fails if a note with no task and no
// initiative appends no note_posted event.
func TestABroadcastNoteWritesNotePosted(t *testing.T) {
	e := newEnv(t)
	var before int
	if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type = 'note_posted'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	k := e.post(t, New{Summary: "registry slow, builds may time out"})
	if !k.Broadcast || k.Task != "" || k.Initiative != "" {
		t.Fatalf("note = %+v, want a broadcast", k)
	}
	var n int
	if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type = 'note_posted' AND ref_id = ?`, k.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("a broadcast note wrote %d note_posted events, want 1", n)
	}
	var total int
	_ = e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type = 'note_posted'`).Scan(&total)
	if total != before+1 {
		t.Errorf("note_posted events %d -> %d, want one more", before, total)
	}
}

func TestPostDerivesTheInitiativeFromTheTaskAndStoresEveryField(t *testing.T) {
	e := newEnv(t)
	k := e.post(t, New{Task: "AA-2", Initiative: "AA", Summary: "  hello  ", Body: "# body\ntext", Resource: " repo:board ",
		Mentions: []string{"codex@host", " ", "codex@host", "nik@host"}})
	if k.Ref != "N1" || k.N != 1 || k.Task != "aa-2" || k.Initiative != "aa" || k.Actor != "claude@host" || k.Summary != "hello" ||
		k.Body != "# body\ntext" || k.Resource != "repo:board" || !k.CreatedAt.Equal(t0.Add(4*time.Second)) || k.Broadcast {
		t.Errorf("note = %+v", k)
	}
	if !reflect.DeepEqual(k.Mentions, []string{"codex@host", "nik@host"}) {
		t.Errorf("mentions = %v", k.Mentions)
	}
	if k.TaskID == "" || k.InitiativeID == "" {
		t.Errorf("ids missing: %+v", k)
	}
	byTask := e.post(t, New{Task: "aa-1", Summary: "s"})
	if byTask.Initiative != "aa" {
		t.Errorf("initiative of a task note = %q, want aa", byTask.Initiative)
	}
	bare := e.post(t, New{Summary: "bare"})
	if bare.Body != "" || bare.Resource != "" || !reflect.DeepEqual(bare.Mentions, []string{}) {
		t.Errorf("bare note = %+v", bare)
	}
	for _, ref := range []string{"N1", "n1", k.ID, strings.ToLower(k.ID)} {
		got, err := e.S.Get(ctx, ref)
		if err != nil || got.ID != k.ID || got.Body != k.Body {
			t.Errorf("Get(%q) = %+v, %v", ref, got, err)
		}
	}
	for _, ref := range []string{"N99", "A1", "", "01J00000000000000000000000"} {
		if _, err := e.S.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", ref, err)
		}
	}
}

func TestPostRefusalsWriteNothing(t *testing.T) {
	e := newEnv(t)
	e.post(t, New{Summary: "first"})
	before := e.counts(t)
	for _, tc := range []struct {
		name string
		in   New
		want error
		text string
	}{
		{"task of another initiative", New{Task: "aa-1", Initiative: "bb", Summary: "s"}, ErrInvalid, "does not belong to initiative bb"},
		{"empty summary", New{Summary: "  "}, ErrInvalid, "summary"},
		{"multi-line summary", New{Summary: "a\nb"}, ErrInvalid, "one line"},
		{"long summary", New{Summary: strings.Repeat("x", 201)}, ErrInvalid, "summary"},
		{"long body", New{Summary: "s", Body: strings.Repeat("x", 20001)}, ErrInvalid, "body"},
		{"long resource", New{Summary: "s", Resource: strings.Repeat("x", 101)}, ErrInvalid, "resource"},
		{"empty supplied resource", New{Summary: "s", ResourceSet: true}, ErrInvalid, "resource"},
		{"blank resource", New{Summary: "s", Resource: "  \t "}, ErrInvalid, "resource"},
		{"blank resource on a task", New{Task: "aa-1", Summary: "s", Resource: " ", ResourceSet: true}, ErrInvalid, "resource"},
		{"long resource padded with spaces", New{Summary: "s", Resource: " " + strings.Repeat("x", 101)}, ErrInvalid, "resource"},
		{"bad mention", New{Summary: "s", Mentions: []string{"a b"}}, ErrInvalid, "mention"},
		{"unknown task", New{Summary: "s", Task: "aa-9"}, ErrNotFound, "aa-9"},
		{"malformed task", New{Summary: "s", Task: "??"}, ErrNotFound, "??"},
		{"unknown initiative", New{Summary: "s", Initiative: "zz"}, ErrNotFound, "zz"},
	} {
		_, err := e.S.Post(ctx, bob, "key:x", tc.in)
		if !errors.Is(err, tc.want) || err == nil || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: err = %v, want %v containing %q", tc.name, err, tc.want, tc.text)
		}
		if got := e.counts(t); got != before {
			t.Errorf("%s: rows %v -> %v, want no change (and no actor)", tc.name, before, got)
		}
	}
	var inv InvalidError
	_, err := e.S.Post(ctx, bob, "key:x", New{Task: "aa-1", Initiative: "bb", Summary: "s"})
	if !errors.As(err, &inv) || inv.Next == "" {
		t.Errorf("a refused task carries a next step, got %+v", inv)
	}
}

// seed posts the notes the list tests read: N1..N7.
func seed(t *testing.T, e env) {
	t.Helper()
	e.post(t, New{Task: "aa-1", Summary: "n1", Body: "b1", Mentions: []string{"codex@host"}})
	e.postAs(t, bob, New{Task: "aa-2", Summary: "n2", Resource: "host:gw1"})
	e.post(t, New{Initiative: "bb", Summary: "n3"})
	e.postAs(t, bob, New{Summary: "n4", Resource: "host:gw1", Mentions: []string{"claude@host", "codex@host"}})
	e.post(t, New{Summary: "n5"})
	e.post(t, New{Task: "bb-1", Summary: "n6", Resource: "repo:r"})
	e.post(t, New{Summary: "n7", Resource: "host:other"})
}

func refs(t *testing.T, e env, q Query) string {
	t.Helper()
	p, err := e.S.List(ctx, q)
	if err != nil {
		t.Fatalf("List(%+v): %v", q, err)
	}
	out := make([]string, len(p.Items))
	for i, k := range p.Items {
		out[i] = k.Ref
	}
	return strings.Join(out, " ")
}

func TestListFiltersCombineAndOrderById(t *testing.T) {
	e := newEnv(t)
	seed(t, e)
	for _, tc := range []struct {
		name string
		q    Query
		want string
	}{
		{"all", Query{}, "N1 N2 N3 N4 N5 N6 N7"},
		{"task", Query{Task: "aa-1"}, "N1"},
		{"task in upper case", Query{Task: "AA-2"}, "N2"},
		{"initiative holds its tasks' notes", Query{Initiative: "aa"}, "N1 N2"},
		{"initiative bb", Query{Initiative: "bb"}, "N3 N6"},
		{"one resource", Query{Resources: []string{"host:gw1"}}, "N2 N4"},
		{"two resources", Query{Resources: []string{"host:gw1", "repo:r"}}, "N2 N4 N6"},
		{"broadcast only", Query{Broadcast: "only"}, "N4 N5 N7"},
		{"broadcast exclude", Query{Broadcast: "exclude"}, "N1 N2 N3 N6"},
		{"mention", Query{Mention: "codex@host"}, "N1 N4"},
		{"mention is exact", Query{Mention: "codex"}, ""},
		{"mention of the other", Query{Mention: "claude@host"}, "N4"},
		{"author", Query{Author: "codex@host"}, "N2 N4"},
		{"author and broadcast", Query{Author: "codex@host", Broadcast: "only"}, "N4"},
		{"resource and exclude", Query{Resources: []string{"host:gw1"}, Broadcast: "exclude"}, "N2"},
	} {
		if got := refs(t, e, tc.q); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestListSinceAcceptsAnIdARefAndATime(t *testing.T) {
	e := newEnv(t)
	seed(t, e)
	n3, _ := e.S.Get(ctx, "N3")
	for _, tc := range []struct {
		name, since, want string
	}{
		{"ref", "N5", "N6 N7"},
		{"ref in lower case", "n6", "N7"},
		{"id", n3.ID, "N4 N5 N6 N7"},
		{"id in lower case", strings.ToLower(n3.ID), "N4 N5 N6 N7"},
		{"time includes the instant", n3.CreatedAt.Format(time.RFC3339), "N3 N4 N5 N6 N7"},
		{"time after all", t0.Add(time.Hour).Format(time.RFC3339), ""},
	} {
		if got := refs(t, e, Query{Since: tc.since}); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := refs(t, e, Query{Since: "N2", Broadcast: "only"}); got != "N4 N5 N7" {
		t.Errorf("since with a filter = %q", got)
	}
}

func TestListPagesByIdAndLeavesBodiesOutUnlessFull(t *testing.T) {
	e := newEnv(t)
	seed(t, e)
	var got []string
	after := ""
	for pages := 0; pages < 10; pages++ {
		p, err := e.S.List(ctx, Query{Limit: 3, After: after})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range p.Items {
			got = append(got, k.Ref)
		}
		if p.NextAfter == "" {
			break
		}
		after = p.NextAfter
	}
	if strings.Join(got, " ") != "N1 N2 N3 N4 N5 N6 N7" {
		t.Errorf("paged = %v", got)
	}
	if p, _ := e.S.List(ctx, Query{Limit: 7}); p.NextAfter != "" || len(p.Items) != 7 {
		t.Errorf("a page ending at the last note = %d items, next %q; want 7 and none", len(p.Items), p.NextAfter)
	}
	if p, _ := e.S.List(ctx, Query{After: "N5"}); len(p.Items) != 2 {
		t.Errorf("after N5 = %d items, want 2", len(p.Items))
	}
	p, _ := e.S.List(ctx, Query{Task: "aa-1"})
	if p.Items[0].Body != "" || !p.Items[0].HasBody {
		t.Errorf("a summary list carries the body %q, has_body %v; want no body and has_body", p.Items[0].Body, p.Items[0].HasBody)
	}
	p, _ = e.S.List(ctx, Query{Task: "aa-1", Full: true})
	if p.Items[0].Body != "b1" {
		t.Errorf("a full list body = %q, want b1", p.Items[0].Body)
	}
	if p, err := e.S.List(ctx, Query{Task: "aa-1", Initiative: "bb"}); err != nil || len(p.Items) != 0 {
		t.Errorf("task in aa and initiative bb = %+v, %v; want no notes", p, err)
	}
}

func TestListRefusals(t *testing.T) {
	e := newEnv(t)
	seed(t, e)
	for _, tc := range []struct {
		name string
		q    Query
		want error
	}{
		{"limit 0 is the default", Query{}, nil},
		{"limit too big", Query{Limit: 1001}, ErrInvalid},
		{"limit negative", Query{Limit: -1}, ErrInvalid},
		{"bad since", Query{Since: "yesterday"}, ErrInvalid},
		{"since an event ref", Query{Since: "E4"}, ErrInvalid},
		{"bad after", Query{After: "xx"}, ErrInvalid},
		{"after a time", Query{After: "2026-11-09T12:00:00Z"}, ErrInvalid},
		{"bad broadcast", Query{Broadcast: "yes"}, ErrInvalid},
		{"unknown task", Query{Task: "aa-9"}, ErrNotFound},
		{"unknown initiative", Query{Initiative: "zz"}, ErrNotFound},
	} {
		_, err := e.S.List(ctx, tc.q)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	var inv InvalidError
	_, err := e.S.List(ctx, Query{Since: "yesterday"})
	if !errors.As(err, &inv) || inv.Next == "" || !strings.Contains(inv.Msg, "yesterday") {
		t.Errorf("a refused since names the value and a next step, got %+v", inv)
	}
}

func TestMigrationCreatesNotesAndCursors(t *testing.T) {
	e := newEnv(t)
	rows, err := e.S.Store.Reader().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'notes' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(n, "sqlite_autoindex") {
			got = append(got, n)
		}
	}
	want := []string{"notes_broadcast", "notes_initiative_id", "notes_resource", "notes_task_id"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("note indexes = %v, want %v", got, want)
	}
	actorID := ""
	if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT id FROM actors LIMIT 1`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	w := e.S.Store.Writer()
	if _, err := w.ExecContext(ctx, `INSERT INTO cursors (actor_id, stream, position) VALUES (?, 'notes', 'P1')`, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO cursors (actor_id, stream, position) VALUES (?, 'notes', 'P2')`, actorID); err == nil {
		t.Error("a second cursor of the same actor and stream was accepted")
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO cursors (actor_id, stream, position) VALUES (?, 'other', 'P2')`, actorID); err == nil {
		t.Error("a cursor on an unknown stream was accepted")
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO cursors (actor_id, stream, position) VALUES (?, 'events', 'P3')`, actorID); err != nil {
		t.Errorf("a cursor on the events stream: %v", err)
	}
}

func TestNormalizeResourceBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   New
		ok   bool
		want string
	}{
		{"omitted", New{Summary: "s"}, true, ""},
		{"one character", New{Summary: "s", Resource: "r", ResourceSet: true}, true, "r"},
		{"one character after trimming", New{Summary: "s", Resource: "  r "}, true, "r"},
		{"one hundred characters", New{Summary: "s", Resource: strings.Repeat("é", 100)}, true, strings.Repeat("é", 100)},
		{"one hundred padded", New{Summary: "s", Resource: " " + strings.Repeat("x", 100) + " "}, true, strings.Repeat("x", 100)},
		{"empty supplied", New{Summary: "s", ResourceSet: true}, false, ""},
		{"blank", New{Summary: "s", Resource: " \t"}, false, ""},
		{"one hundred and one", New{Summary: "s", Resource: strings.Repeat("é", 101)}, false, ""},
	} {
		got, err := tc.in.Normalize()
		if (err == nil) != tc.ok || (tc.ok && got.Resource != tc.want) {
			t.Errorf("%s: resource %q, err %v; want ok=%v resource %q", tc.name, got.Resource, err, tc.ok, tc.want)
		}
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
}
