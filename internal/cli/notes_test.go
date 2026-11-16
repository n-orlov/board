package cli

import (
	"testing"
	"time"
)

func TestNoteLineScopeResourceAndBodyMarker(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 10, 0, 0, time.UTC)
	base := noteView{Ref: "N7", Author: "me", Summary: "found it", CreatedAt: "2026-03-04T12:00:00Z"}
	for _, c := range []struct {
		name string
		edit func(*noteView)
		want string
	}{
		{"broadcast", func(*noteView) {}, "N7  10m ago  me  broadcast  found it"},
		{"task wins over initiative", func(n *noteView) { n.Task, n.Initiative = "aa-1", "aa" }, "N7  10m ago  me  aa-1  found it"},
		{"initiative", func(n *noteView) { n.Initiative = "bb" }, "N7  10m ago  me  bb  found it"},
		{"resource and body", func(n *noteView) { n.Resource, n.HasBody = "host:x", true }, "N7  10m ago  me  broadcast  [host:x]  found it  +"},
	} {
		n := base
		c.edit(&n)
		if got := noteLine(n, now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNoteDetailFactsAndBody(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 10, 0, 0, time.UTC)
	n := noteView{Ref: "N1", Author: "me", Summary: "s", CreatedAt: "2026-03-04T12:00:00Z", Task: "aa-1", Initiative: "aa", Mentions: []string{"x", "y"}, HasBody: true, Body: "text"}
	want := "N1  10m ago  me  aa-1  s  +\ninitiative: aa\nmentions: x, y\ntext\n"
	if got := noteDetail(n, now); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	n.Body = "text\n"
	if got := noteDetail(n, now); got != want {
		t.Errorf("a body ending in a newline: got %q, want %q", got, want)
	}
}

func TestNoteSinceForms(t *testing.T) {
	for in, want := range map[string]string{
		"":                     "",
		"last":                 "cursor",
		"n12":                  "N12",
		"N3":                   "N3",
		"2026-03-04":           "2026-03-04T00:00:00Z",
		"2026-03-04T09:00:00Z": "2026-03-04T09:00:00Z",
		"01JABCDEF":            "01JABCDEF",
	} {
		if got := noteSince(in); got != want {
			t.Errorf("noteSince(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoteQueryValuesAndCommand(t *testing.T) {
	q := noteQuery{since: "cursor", task: "aa-1", initiative: "aa", mention: "nik", broadcast: true, ack: true, resources: []string{"host:x", "host:y"}}
	v := q.values()
	if v.Get("since") != "cursor" || v.Get("task") != "aa-1" || v.Get("initiative") != "aa" || v.Get("mention") != "nik" || v.Get("broadcast") != "only" {
		t.Errorf("values: %v", v)
	}
	if got := v["resource"]; len(got) != 2 || got[0] != "host:x" || got[1] != "host:y" {
		t.Errorf("resource: %v", got)
	}
	if v.Has("ack") {
		t.Errorf("ack must not be sent: the cursor moves past the shown notes only: %v", v)
	}
	want := "vh notes --since last --task aa-1 --init aa --mention nik --broadcast --res host:x --res host:y --ack"
	if got := q.command(); got != want {
		t.Errorf("command: got %q, want %q", got, want)
	}
	if got := (noteQuery{}).command(); got != "vh notes" {
		t.Errorf("empty command: %q", got)
	}
	if got := (noteQuery{}).values(); len(got) != 0 {
		t.Errorf("empty values: %v", got)
	}
}

func TestNotesCapDefaultsToFive(t *testing.T) {
	for _, c := range []struct {
		args []string
		want int
		ok   bool
	}{
		{nil, 5, true},
		{[]string{"--limit", "2"}, 2, true},
		{[]string{"--all"}, 0, true},
		{[]string{"--limit", "0"}, 0, false},
		{[]string{"--all", "--limit", "2"}, 0, false},
	} {
		p, _ := parseArgs(c.args, notesFlags)
		got, ok := notesCap(p)
		if got != c.want || ok != c.ok {
			t.Errorf("%v: got %d,%v want %d,%v", c.args, got, ok, c.want, c.ok)
		}
	}
}
