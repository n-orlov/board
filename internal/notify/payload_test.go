package notify_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/notify"
	"github.com/n-orlov/board/internal/store"
	"github.com/n-orlov/board/internal/tasks"
)

var holder = audit.Actor{Handle: "claude@host", WayBack: "tmux attach -t work"}

// payloads is every notification of the env's outbox, oldest first, as raw JSON.
func (e env) payloads(t *testing.T) []string {
	t.Helper()
	rs, err := e.st.Reader().QueryContext(ctx, `SELECT payload FROM notifications ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	var out []string
	for rs.Next() {
		var p string
		if err := rs.Scan(&p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// last is the newest payload decoded twice: into the documented shape and into a map, to see which keys are there.
func (e env) last(t *testing.T) (notify.Payload, map[string]any) {
	t.Helper()
	all := e.payloads(t)
	raw := all[len(all)-1]
	var p notify.Payload
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return p, m
}

func wantKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
		if _, ok := m[k]; !ok {
			t.Errorf("payload has no %q: %v", k, m)
		}
	}
	for k := range m {
		if !want[k] {
			t.Errorf("payload has the unexpected key %q: %v", k, m)
		}
	}
}

func wantText(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(text, p) {
			t.Errorf("text %q does not name %q", text, p)
		}
	}
}

func (e env) expireLease(t *testing.T, a audit.Actor) {
	t.Helper()
	ttl := int64(60)
	if _, err := e.leases.Start(ctx, a, "key:x", leases.Start{Task: "aa-1", Intent: "write the parser", TTL: &ttl}); err != nil {
		t.Fatal(err)
	}
	e.tick(2 * time.Minute)
	if n, err := e.leases.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireDue = %d, %v", n, err)
	}
}

func TestStrandedPayloadNamesTheTaskTheLeaseAndTheWayBack(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.st.SetPublicURL("https://board.example/")
	e.expireLease(t, holder)
	p, m := e.last(t)
	wantKeys(t, m, "text", "kind", "task", "actor", "lease", "board_url")
	if p.Kind != "stranded" || p.Task == nil || *p.Task != (notify.TaskInfo{Ref: "aa-1", Title: "t", URL: "https://board.example/#/t/aa-1"}) {
		t.Fatalf("kind/task = %q %+v", p.Kind, p.Task)
	}
	if p.Actor != (notify.Actor{Handle: "claude@host", WayBack: "tmux attach -t work"}) {
		t.Fatalf("actor = %+v", p.Actor)
	}
	if p.Lease == nil || p.Lease.Ref != "L1" || p.Lease.Intent != "write the parser" || p.Lease.ExpiredAt != "2026-11-05T12:01:00Z" {
		t.Fatalf("lease = %+v", p.Lease)
	}
	if p.BoardURL != "https://board.example/" {
		t.Fatalf("board_url = %q", p.BoardURL)
	}
	wantText(t, p.Text, "stranded", "aa-1", "t", "L1", "claude@host", "write the parser", "tmux attach -t work")
	if n := strings.Count(p.Text, "\n"); n != 1 {
		t.Fatalf("text has %d line breaks, want two lines: %q", n, p.Text)
	}
}

func TestStrandedPayloadSaysWhenNoWayBackIsRecorded(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.expireLease(t, audit.Actor{Handle: "claude@host"})
	p, m := e.last(t)
	actor := m["actor"].(map[string]any)
	if _, ok := actor["way_back"]; ok || p.Actor.WayBack != "" {
		t.Fatalf("actor = %v", actor)
	}
	wantText(t, p.Text, "way back: none recorded")
}

func TestDonePayload(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.st.SetPublicURL("https://board.example")
	if _, err := e.tasks.Done(ctx, holder, "key:x", "aa-2", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	p, m := e.last(t)
	wantKeys(t, m, "text", "kind", "task", "actor", "board_url")
	if p.Kind != "done" || p.Task == nil || p.Task.Ref != "aa-2" || p.Task.Title != "t" || p.Task.URL != "https://board.example/#/t/aa-2" {
		t.Fatalf("kind/task = %q %+v", p.Kind, p.Task)
	}
	if p.Actor.Handle != "claude@host" || p.BoardURL != "https://board.example/" {
		t.Fatalf("actor/board_url = %+v %q", p.Actor, p.BoardURL)
	}
	wantText(t, p.Text, "done", "aa-2", "t", "claude@host")
}

func TestAskingPayloadOnATaskAnInitiativeAndTheBoard(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	e.st.SetPublicURL("https://board.example")
	for _, c := range []struct {
		name    string
		in      asks.New
		keys    []string
		scope   string
		task    string
		initSlg string
	}{
		{"task", asks.New{Task: "aa-1", Question: "which parser?", Blocking: true}, []string{"text", "kind", "task", "actor", "ask", "board_url"}, "aa-1", "aa-1", ""},
		{"initiative", asks.New{Initiative: "aa", Question: "which scope?"}, []string{"text", "kind", "initiative", "actor", "ask", "board_url"}, "initiative aa", "", "aa"},
		{"board", asks.New{Question: "which board?"}, []string{"text", "kind", "actor", "ask", "board_url"}, "board", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := e.asks.Raise(ctx, holder, "key:x", c.in); err != nil {
				t.Fatal(err)
			}
			p, m := e.last(t)
			wantKeys(t, m, c.keys...)
			if p.Kind != "asking" || p.Ask == nil || p.Ask.Question != c.in.Question || p.Ask.Blocking != c.in.Blocking || !strings.HasPrefix(p.Ask.Ref, "A") {
				t.Fatalf("kind/ask = %q %+v", p.Kind, p.Ask)
			}
			if (p.Task == nil) != (c.task == "") || (p.Task != nil && p.Task.Ref != c.task) {
				t.Fatalf("task = %+v, want %q", p.Task, c.task)
			}
			if (p.Initiative == nil) != (c.initSlg == "") || (p.Initiative != nil && p.Initiative.Slug != c.initSlg) {
				t.Fatalf("initiative = %+v, want %q", p.Initiative, c.initSlg)
			}
			if p.Actor.Handle != "claude@host" || p.BoardURL != "https://board.example/" {
				t.Fatalf("actor/board_url = %+v %q", p.Actor, p.BoardURL)
			}
			wantText(t, p.Text, "asking", c.scope, c.in.Question, "claude@host")
			if c.in.Blocking {
				wantText(t, p.Text, "blocking")
			}
		})
	}
}

func TestPayloadHasNoUrlsWithoutAPublicURL(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	if _, err := e.tasks.Done(ctx, holder, "key:x", "aa-2", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	e.expireLease(t, holder)
	for _, raw := range e.payloads(t) {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["board_url"]; ok {
			t.Errorf("board_url present: %s", raw)
		}
		if _, ok := m["task"].(map[string]any)["url"]; ok {
			t.Errorf("task url present: %s", raw)
		}
	}
}

func TestTextIsOneLineAndShort(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	long := "line one    line   two " + strings.Repeat("x", 150)
	if _, err := e.asks.Raise(ctx, holder, "key:x", asks.New{Question: long}); err != nil {
		t.Fatal(err)
	}
	p, _ := e.last(t)
	if strings.Contains(p.Text, "\n") || !strings.Contains(p.Text, "line one line two x") || !strings.Contains(p.Text, "…") || len([]rune(p.Text)) > 200 {
		t.Fatalf("text = %q", p.Text)
	}
	if p.Ask.Question != long {
		t.Fatalf("the ask's question was cut: %q", p.Ask.Question)
	}
}

func TestNoPayloadCarriesAKeyASessionOrTheHeaderValue(t *testing.T) {
	const header = "X-Receiver-Token: hunter2-receiver-token"
	t.Setenv("BOARD_NOTIFY_HEADER", header)
	e := newEnv(t, store.NotifyKinds)
	e.st.SetPublicURL("https://board.example")
	raw, _, err := (auth.Keys{Store: e.st}).Create(ctx, "agent-key", "operator", 0)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := (auth.Sessions{Store: e.st}).Create(ctx, "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	e.expireLease(t, holder)
	if _, err := e.tasks.Done(ctx, holder, "key:x", "aa-2", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []asks.New{{Task: "aa-1", Question: "q"}, {Initiative: "aa", Question: "q"}, {Question: "q"}} {
		if _, err := e.asks.Raise(ctx, holder, "key:x", in); err != nil {
			t.Fatal(err)
		}
	}
	all := e.payloads(t)
	if len(all) != 5 {
		t.Fatalf("%d payloads, want the five cases", len(all))
	}
	for _, p := range all {
		for name, secret := range map[string]string{"api key": raw, "session id": session, "header value": "hunter2-receiver-token", "header": header} {
			if secret == "" || strings.Contains(p, secret) {
				t.Errorf("the %s (%d bytes) is empty or in the payload %s", name, len(secret), p)
			}
		}
		for _, word := range []string{"key", "session", "header", "token"} {
			if strings.Contains(strings.ToLower(p), word) {
				t.Errorf("the payload names %q: %s", word, p)
			}
		}
	}
}

func TestTextCutsAFieldPastOneHundredTwentyRunes(t *testing.T) {
	e := newEnv(t, store.NotifyKinds)
	for _, c := range []struct {
		n    int
		want string
	}{{120, strings.Repeat("é", 120)}, {121, strings.Repeat("é", 119) + "…"}} {
		if _, err := e.asks.Raise(ctx, holder, "key:x", asks.New{Question: strings.Repeat("é", c.n)}); err != nil {
			t.Fatal(err)
		}
		if p, _ := e.last(t); !strings.Contains(p.Text, ": "+c.want+" (") {
			t.Errorf("a question of %d runes gave the text %q", c.n, p.Text)
		}
	}
}
