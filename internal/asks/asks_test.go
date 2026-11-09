package asks

import (
	"context"
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
	op  = audit.Actor{Handle: "nik@host", Operator: true}
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type env struct {
	S     Store
	Tasks tasks.Store
	Clock *clock
}

// newEnv is a migrated SQLite database with open initiatives aa and bb, tasks
// aa-1, aa-2 (closed) and bb-1, a closed initiative cc and a moving clock.
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
	for _, slug := range []string{"aa", "bb", "cc"} {
		if _, err := ini.Create(ctx, me, "key:x", initiatives.New{Slug: slug, Title: "I"}); err != nil {
			t.Fatal(err)
		}
	}
	e := env{S: Store{Store: st, Now: c.Now}, Tasks: tasks.Store{Store: st, Now: c.Now}, Clock: c}
	for _, in := range []string{"aa", "aa", "bb"} {
		c.now = c.now.Add(time.Second)
		if _, err := e.Tasks.Create(ctx, me, "key:x", tasks.New{Initiative: in, Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Tasks.Close(ctx, me, "key:x", "aa-2", "dropped"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Tasks.CloseInitiative(ctx, me, "key:x", "cc", false); err != nil {
		t.Fatal(err)
	}
	return e
}

// raise raises an ask as me, one second after the last call.
func (e env) raise(t *testing.T, in New) Ask {
	t.Helper()
	e.Clock.now = e.Clock.now.Add(time.Second)
	k, err := e.S.Raise(ctx, me, "key:x", in)
	if err != nil {
		t.Fatalf("raise %+v: %v", in, err)
	}
	return k
}

// counts is the number of asks, events and actors rows.
func (e env) counts(t *testing.T) [3]int {
	t.Helper()
	var out [3]int
	for i, table := range []string{"asks", "events", "actors"} {
		if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func (e env) lastEvent(t *testing.T) (typ, principal, summary string) {
	t.Helper()
	err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT type, principal, summary FROM events ORDER BY n DESC LIMIT 1`).Scan(&typ, &principal, &summary)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestRaiseOnATaskDerivesItsInitiativeAndAppendsOneEvent(t *testing.T) {
	e := newEnv(t)
	before := e.counts(t)
	k := e.raise(t, New{Task: "AA-1", Question: " Which db? ", Details: "## why", Options: []string{"sqlite", "pg"}, Mode: "poll", Blocking: true})
	if k.Ref != "A1" || k.Task != "aa-1" || k.Initiative != "aa" || k.Actor != me.Handle || k.Question != "Which db?" ||
		k.State != "open" || k.Mode != "poll" || !k.Blocking || !reflect.DeepEqual(k.Options, []string{"sqlite", "pg"}) ||
		k.Details != "## why" || k.PollSeenAt != nil || k.AnsweredAt != nil || k.Answer != "" {
		t.Fatalf("ask = %+v", k)
	}
	after := e.counts(t)
	if after[0] != before[0]+1 || after[1] != before[1]+1 {
		t.Errorf("counts %v -> %v, want one ask and one event more", before, after)
	}
	typ, principal, summary := e.lastEvent(t)
	if typ != EventRaised || principal != "key:x" || summary != "ask A1 raised on aa-1: Which db?" {
		t.Errorf("event = %s %s %q", typ, principal, summary)
	}
	var task, ini, ref string
	if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT task_id, initiative_id, ref_id FROM events ORDER BY n DESC LIMIT 1`).Scan(&task, &ini, &ref); err != nil {
		t.Fatal(err)
	}
	if task != k.TaskID || ini != k.InitiativeID || ref != k.ID {
		t.Errorf("event ids = %s %s %s, ask %+v", task, ini, ref, k)
	}
}

func TestRaiseOnAnInitiativeAndGlobalAndDefaults(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Initiative: "BB", Question: "q"})
	if k.Task != "" || k.Initiative != "bb" || k.Mode != "stop" || k.Blocking || k.Options == nil || len(k.Options) != 0 {
		t.Errorf("initiative ask = %+v", k)
	}
	if _, _, s := e.lastEvent(t); s != "ask A1 raised on bb: q" {
		t.Errorf("summary = %q", s)
	}
	g := e.raise(t, New{Question: "global"})
	if g.Task != "" || g.Initiative != "" || g.TaskID != "" || g.InitiativeID != "" {
		t.Errorf("global ask = %+v", g)
	}
	if _, _, s := e.lastEvent(t); s != "ask A2 raised: global" {
		t.Errorf("summary = %q", s)
	}
	both := e.raise(t, New{Task: "aa-1", Initiative: "aa", Question: "same"})
	if both.Task != "aa-1" || both.Initiative != "aa" {
		t.Errorf("task and its initiative = %+v", both)
	}
}

func TestRaiseRefusalsWriteNothing(t *testing.T) {
	e := newEnv(t)
	long := strings.Repeat("x", 201)
	many := make([]string, 11)
	for i := range many {
		many[i] = "o"
	}
	for name, c := range map[string]struct {
		in   New
		want error
	}{
		"empty question":     {New{Question: "  "}, ErrInvalid},
		"long question":      {New{Question: long}, ErrInvalid},
		"two lines":          {New{Question: "a\nb"}, ErrInvalid},
		"long details":       {New{Question: "q", Details: strings.Repeat("x", 20001)}, ErrInvalid},
		"eleven options":     {New{Question: "q", Options: many}, ErrInvalid},
		"empty option":       {New{Question: "q", Options: []string{""}}, ErrInvalid},
		"long option":        {New{Question: "q", Options: []string{strings.Repeat("x", 81)}}, ErrInvalid},
		"bad mode":           {New{Question: "q", Mode: "shout"}, ErrInvalid},
		"task and other ini": {New{Question: "q", Task: "aa-1", Initiative: "bb"}, ErrInvalid},
		"unknown task":       {New{Question: "q", Task: "aa-9"}, ErrNotFound},
		"unreadable task":    {New{Question: "q", Task: "??"}, ErrNotFound},
		"unknown initiative": {New{Question: "q", Initiative: "zz"}, ErrNotFound},
		"closed task":        {New{Question: "q", Task: "aa-2"}, ErrConflict},
		"closed initiative":  {New{Question: "q", Initiative: "cc"}, ErrConflict},
	} {
		before := e.counts(t)
		_, err := e.S.Raise(ctx, bob, "key:y", c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
		if got := e.counts(t); got != before {
			t.Errorf("%s: counts %v -> %v", name, before, got)
		}
		var inv InvalidError
		var con ConflictError
		if (errors.As(err, &inv) && inv.Next == "") || (errors.As(err, &con) && con.Next == "") {
			t.Errorf("%s: no next step in %v", name, err)
		}
	}
}

func TestGetReadsByRefAndUlidInEitherCase(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Question: "q"})
	for _, ref := range []string{"A1", "a1", k.ID, strings.ToLower(k.ID)} {
		got, err := e.S.Get(ctx, ref)
		if err != nil || got.ID != k.ID {
			t.Errorf("Get(%q) = %+v, %v", ref, got, err)
		}
	}
	for _, ref := range []string{"A2", "A0", "x", "", "L1", strings.Repeat("0", 26)} {
		if _, err := e.S.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", ref, err)
		}
	}
}

func TestListFiltersAndPaging(t *testing.T) {
	e := newEnv(t)
	a1 := e.raise(t, New{Task: "aa-1", Question: "1"})
	e.Clock.now = e.Clock.now.Add(time.Second)
	b, err := e.S.Raise(ctx, bob, "key:y", New{Initiative: "bb", Question: "2"})
	if err != nil {
		t.Fatal(err)
	}
	a3 := e.raise(t, New{Question: "3"})
	a4 := e.raise(t, New{Task: "bb-1", Question: "4"})
	if _, err := e.S.Answer(ctx, op, "oidc:n", a3.Ref, Reply{Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.S.Withdraw(ctx, me, "key:x", a4.Ref, ""); err != nil {
		t.Fatal(err)
	}
	refs := func(q Query) string {
		t.Helper()
		p, err := e.S.List(ctx, q)
		if err != nil {
			t.Fatalf("List(%+v): %v", q, err)
		}
		var out []string
		for _, k := range p.Items {
			out = append(out, k.Ref)
		}
		return strings.Join(out, " ")
	}
	for name, c := range map[string]struct {
		q    Query
		want string
	}{
		"default is open":   {Query{}, "A1 A2"},
		"open":              {Query{State: "open"}, "A1 A2"},
		"answered":          {Query{State: "answered"}, "A3"},
		"withdrawn":         {Query{State: "withdrawn"}, "A4"},
		"all":               {Query{State: "all"}, "A1 A2 A3 A4"},
		"actor":             {Query{State: "all", Actor: bob.Handle}, "A2"},
		"task":              {Query{State: "all", Task: "AA-1"}, "A1"},
		"task by id":        {Query{State: "all", Task: a1.TaskID}, "A1"},
		"initiative":        {Query{State: "all", Initiative: "bb"}, "A2 A4"},
		"initiative of ask": {Query{State: "all", Initiative: "aa"}, "A1"},
		"after ref":         {Query{State: "all", After: "a2"}, "A3 A4"},
		"after ulid":        {Query{State: "all", After: strings.ToLower(b.ID)}, "A3 A4"},
		"after unknown id":  {Query{State: "all", After: strings.Repeat("0", 26)}, "A1 A2 A3 A4"},
		"limit":             {Query{State: "all", Limit: 3}, "A1 A2 A3"},
	} {
		if got := refs(c.q); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	p, err := e.S.List(ctx, Query{State: "all", Limit: 3})
	if err != nil || p.NextAfter != a3.ID {
		t.Errorf("first page next = %q, %v; want %s", p.NextAfter, err, a3.ID)
	}
	p, err = e.S.List(ctx, Query{State: "all", Limit: 3, After: p.NextAfter})
	if err != nil || len(p.Items) != 1 || p.NextAfter != "" {
		t.Errorf("last page = %+v, %v", p, err)
	}
	for name, q := range map[string]Query{
		"state":         {State: "done"},
		"limit":         {Limit: 1001},
		"after text":    {After: "zzz"},
		"after missing": {After: "A99"},
	} {
		if _, err := e.S.List(ctx, q); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	for name, q := range map[string]Query{"task": {Task: "aa-9"}, "initiative": {Initiative: "zz"}} {
		if _, err := e.S.List(ctx, q); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestAnswerRecordsTheChannelAndAppendsOneEvent(t *testing.T) {
	e := newEnv(t)
	for name, c := range map[string]struct {
		actor   audit.Actor
		reply   Reply
		channel string
		behalf  string
	}{
		"session default": {op, Reply{Answer: "yes"}, "ui", ""},
		"key default":     {bob, Reply{Answer: "yes"}, "api", ""},
		"explicit api":    {op, Reply{Answer: "yes", Channel: "api"}, "api", ""},
		"relay":           {bob, Reply{Answer: " go ", Channel: "relay", OnBehalfOf: " nik@phone "}, "relay", "nik@phone"},
	} {
		k := e.raise(t, New{Task: "aa-1", Question: "q"})
		before := e.counts(t)
		got, err := e.S.Answer(ctx, c.actor, "p:"+name, strings.ToLower(k.Ref), c.reply)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.State != "answered" || got.Answer != strings.TrimSpace(c.reply.Answer) || got.Channel != c.channel ||
			got.AnsweredBy != c.actor.Handle || got.OnBehalfOf != c.behalf || got.AnsweredAt == nil {
			t.Errorf("%s: ask = %+v", name, got)
		}
		after := e.counts(t)
		if after[0] != before[0] || after[1] != before[1]+1 {
			t.Errorf("%s: counts %v -> %v, want one event more", name, before, after)
		}
		typ, principal, summary := e.lastEvent(t)
		if typ != EventAnswered || principal != "p:"+name || summary != "ask "+k.Ref+" answered on aa-1: "+got.Answer {
			t.Errorf("%s: event = %s %s %q", name, typ, principal, summary)
		}
		if c.behalf != "" {
			var n int
			if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM events e JOIN actors a ON a.id = e.on_behalf_of WHERE a.handle = ?`, c.behalf).Scan(&n); err != nil || n != 1 {
				t.Errorf("%s: events on behalf of %s = %d, %v", name, c.behalf, n, err)
			}
		}
	}
}

func TestAnswerRefusalsWriteNothing(t *testing.T) {
	e := newEnv(t)
	open := e.raise(t, New{Question: "q"})
	done := e.raise(t, New{Question: "q"})
	gone := e.raise(t, New{Question: "q"})
	if _, err := e.S.Answer(ctx, op, "p", done.Ref, Reply{Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.S.Withdraw(ctx, me, "p", gone.Ref, "no longer"); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		ref   string
		reply Reply
		want  error
		text  string
	}{
		"answered":        {done.Ref, Reply{Answer: "again"}, ErrConflict, "it is answered"},
		"withdrawn":       {gone.Ref, Reply{Answer: "again"}, ErrConflict, "it is withdrawn"},
		"unknown":         {"A99", Reply{Answer: "x"}, ErrNotFound, "A99"},
		"empty":           {open.Ref, Reply{Answer: " "}, ErrInvalid, "answer"},
		"long":            {open.Ref, Reply{Answer: strings.Repeat("x", 20001)}, ErrInvalid, "answer"},
		"bad channel":     {open.Ref, Reply{Answer: "x", Channel: "sms"}, ErrInvalid, "channel"},
		"behalf no relay": {open.Ref, Reply{Answer: "x", Channel: "api", OnBehalfOf: "a@b"}, ErrInvalid, "on_behalf_of"},
		"behalf default":  {open.Ref, Reply{Answer: "x", OnBehalfOf: "a@b"}, ErrInvalid, "on_behalf_of"},
	} {
		before := e.counts(t)
		_, err := e.S.Answer(ctx, bob, "key:y", c.ref, c.reply)
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.text) {
			t.Errorf("%s: err = %v, want %v naming %q", name, err, c.want, c.text)
		}
		if got := e.counts(t); got != before {
			t.Errorf("%s: counts %v -> %v: a refusal wrote a row, event or actor", name, before, got)
		}
	}
	if k, err := e.S.Get(ctx, done.Ref); err != nil || k.Answer != "yes" || k.AnsweredBy != op.Handle {
		t.Errorf("refused answer changed the ask: %+v, %v", k, err)
	}
}

func TestWithdrawRecordsTheReasonAndAppendsOneEvent(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Task: "aa-1", Question: "q"})
	before := e.counts(t)
	got, err := e.S.Withdraw(ctx, bob, "key:y", k.ID, "  not needed \n now ")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "withdrawn" || got.WithdrawReason != "not needed \n now" || got.WithdrawnAt == nil || got.AnsweredAt != nil || got.Answer != "" {
		t.Errorf("ask = %+v", got)
	}
	if after := e.counts(t); after[0] != before[0] || after[1] != before[1]+1 {
		t.Errorf("counts %v -> %v", before, after)
	}
	if typ, principal, summary := e.lastEvent(t); typ != EventWithdrawn || principal != "key:y" || summary != "ask A1 withdrawn on aa-1: not needed now" {
		t.Errorf("event = %s %s %q", typ, principal, summary)
	}
	k2 := e.raise(t, New{Question: "q2"})
	if got, err := e.S.Withdraw(ctx, me, "key:x", k2.Ref, ""); err != nil || got.WithdrawReason != "" {
		t.Errorf("withdraw without a reason = %+v, %v", got, err)
	}
	if _, _, summary := e.lastEvent(t); summary != "ask A2 withdrawn" {
		t.Errorf("summary = %q", summary)
	}
}

func TestWithdrawRefusalsWriteNothing(t *testing.T) {
	e := newEnv(t)
	open := e.raise(t, New{Question: "q"})
	done := e.raise(t, New{Question: "q"})
	gone := e.raise(t, New{Question: "q"})
	if _, err := e.S.Answer(ctx, op, "p", done.Ref, Reply{Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.S.Withdraw(ctx, me, "p", gone.Ref, ""); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		ref, reason string
		want        error
		text        string
	}{
		"answered":    {done.Ref, "", ErrConflict, "it is answered"},
		"withdrawn":   {gone.Ref, "", ErrConflict, "it is withdrawn"},
		"unknown":     {"A99", "", ErrNotFound, "A99"},
		"long reason": {open.Ref, strings.Repeat("x", 201), ErrInvalid, "reason"},
	} {
		before := e.counts(t)
		_, err := e.S.Withdraw(ctx, bob, "key:y", c.ref, c.reason)
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.text) {
			t.Errorf("%s: err = %v, want %v naming %q", name, err, c.want, c.text)
		}
		if got := e.counts(t); got != before {
			t.Errorf("%s: counts %v -> %v: a refusal wrote a row, event or actor", name, before, got)
		}
	}
}

// TestALostRaceNamesTheStateAnotherWriterMovedTheAskTo covers the conditional
// update: an ask read as open that was answered meanwhile is not overwritten.
func TestALostRaceNamesTheStateAnotherWriterMovedTheAskTo(t *testing.T) {
	e := newEnv(t)
	k := e.raise(t, New{Question: "q"})
	if _, err := e.S.Answer(ctx, op, "p", k.Ref, Reply{Answer: "first"}); err != nil {
		t.Fatal(err)
	}
	tx, err := e.S.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE asks SET state = 'answered', answer = 'second' WHERE id = ? AND state = 'open'`, k.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = e.S.wonRace(ctx, tx, res, k, "answer")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "it is answered") {
		t.Errorf("wonRace = %v", err)
	}
	if got, _ := e.S.find(ctx, tx, k.ID); got.Answer != "first" {
		t.Errorf("answer = %q", got.Answer)
	}
	if err := e.S.wonRace(ctx, tx, res, Ask{ID: "NOPE", Ref: "A9"}, "answer"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wonRace on a vanished ask = %v", err)
	}
}

func TestWaitingNeedsAnOpenPollAskSeenWithinTheLastMinute(t *testing.T) {
	s := Store{Now: func() time.Time { return t0 }}
	seen := func(ago time.Duration) *time.Time { at := t0.Add(-ago); return &at }
	for name, tc := range map[string]struct {
		ask  Ask
		want bool
	}{
		"polled just now": {Ask{State: "open", Mode: "poll", PollSeenAt: seen(0)}, true},
		"polled 60 s ago": {Ask{State: "open", Mode: "poll", PollSeenAt: seen(60 * time.Second)}, true},
		"polled 61 s ago": {Ask{State: "open", Mode: "poll", PollSeenAt: seen(61 * time.Second)}, false},
		"never polled":    {Ask{State: "open", Mode: "poll"}, false},
		"stop mode":       {Ask{State: "open", Mode: "stop", PollSeenAt: seen(0)}, false},
		"answered":        {Ask{State: "answered", Mode: "poll", PollSeenAt: seen(0)}, false},
		"withdrawn":       {Ask{State: "withdrawn", Mode: "poll", PollSeenAt: seen(0)}, false},
	} {
		if got := s.Waiting(tc.ask); got != tc.want {
			t.Errorf("%s: Waiting = %v, want %v", name, got, tc.want)
		}
	}
}
