package asks

import (
	"strings"
	"testing"
	"time"
)

func refs(items []Ask) string {
	out := make([]string, len(items))
	for i, k := range items {
		out[i] = k.Ref
	}
	return strings.Join(out, ",")
}

func TestMineListsMyOpenAsksAndMyAnswersSinceTheBound(t *testing.T) {
	e := newEnv(t)
	empty, err := e.S.Mine(ctx, me.Handle, t0)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("Mine on an empty board = %v, %v; want an empty list", empty, err)
	}
	open := e.raise(t, New{Task: "aa-1", Question: "open"})
	old := e.raise(t, New{Task: "aa-1", Question: "old"})
	if _, err := e.S.Answer(ctx, op, "p", old.Ref, Reply{Answer: "no"}); err != nil {
		t.Fatal(err)
	}
	answeredAt := e.Clock.now
	e.Clock.now = e.Clock.now.Add(time.Second)
	fresh := e.raise(t, New{Task: "aa-1", Question: "fresh"})
	if _, err := e.S.Answer(ctx, op, "p", fresh.Ref, Reply{Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	gone := e.raise(t, New{Task: "aa-1", Question: "gone"})
	if _, err := e.S.Withdraw(ctx, me, "p", gone.Ref, "no longer"); err != nil {
		t.Fatal(err)
	}
	e.Clock.now = e.Clock.now.Add(time.Second)
	if _, err := e.S.Raise(ctx, bob, "p", New{Task: "aa-1", Question: "not mine"}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		since time.Time
		want  string
	}{
		"bound before every answer":  {answeredAt.Add(-time.Hour), open.Ref + "," + old.Ref + "," + fresh.Ref},
		"bound at the first answer":  {answeredAt, open.Ref + "," + old.Ref + "," + fresh.Ref},
		"bound just after the first": {answeredAt.Add(time.Millisecond), open.Ref + "," + fresh.Ref},
		"bound after every answer":   {answeredAt.Add(time.Hour), open.Ref},
	} {
		got, err := e.S.Mine(ctx, me.Handle, c.since)
		if err != nil || refs(got) != c.want {
			t.Errorf("%s: Mine = %s, %v; want %s", name, refs(got), err, c.want)
		}
	}
	got, _ := e.S.Mine(ctx, me.Handle, answeredAt)
	if got[1].Answer != "no" || got[1].AnsweredBy != op.Handle || got[1].AnsweredAt == nil {
		t.Errorf("an answered ask carries its answer: %+v", got[1])
	}
	if theirs, _ := e.S.Mine(ctx, bob.Handle, answeredAt); len(theirs) != 1 || theirs[0].Question != "not mine" {
		t.Errorf("Mine for another actor = %v", theirs)
	}
}

func TestAnsweredBetweenAndSinceListMyAnswersByTheirEvents(t *testing.T) {
	e := newEnv(t)
	first := e.raise(t, New{Task: "aa-1", Question: "first"})
	second := e.raise(t, New{Task: "aa-1", Question: "second"})
	theirs, err := e.S.Raise(ctx, bob, "p", New{Task: "aa-1", Question: "theirs"})
	if err != nil {
		t.Fatal(err)
	}
	e.raise(t, New{Task: "aa-1", Question: "open"})
	var (
		through []int64
		at      []time.Time
	)
	for _, k := range []Ask{first, theirs, second} {
		e.Clock.now = e.Clock.now.Add(time.Hour)
		if _, err := e.S.Answer(ctx, op, "p", k.Ref, Reply{Answer: "ok"}); err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT MAX(n) FROM events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		through = append(through, n)
		at = append(at, e.Clock.now)
	}
	list := func(items []Ask, err error) string {
		t.Helper()
		if err != nil || items == nil {
			t.Fatalf("list = %v, %v", items, err)
		}
		return refs(items)
	}
	if got := list(e.S.AnsweredBetween(ctx, me.Handle, 0, through[2])); got != first.Ref+","+second.Ref {
		t.Errorf("between 0 and the last = %s, want mine in order", got)
	}
	if got := list(e.S.AnsweredBetween(ctx, me.Handle, through[0], through[2])); got != second.Ref {
		t.Errorf("after the first answer = %s, want only %s", got, second.Ref)
	}
	if got := list(e.S.AnsweredBetween(ctx, me.Handle, 0, through[1])); got != first.Ref {
		t.Errorf("up to the middle answer = %s, want only %s", got, first.Ref)
	}
	if got := list(e.S.AnsweredBetween(ctx, me.Handle, through[2], through[2])); got != "" {
		t.Errorf("after the last answer = %q, want none", got)
	}
	if got := list(e.S.AnsweredSince(ctx, me.Handle, at[1].Add(time.Millisecond), through[2])); got != second.Ref {
		t.Errorf("since just after the middle answer = %s, want %s", got, second.Ref)
	}
	if got := list(e.S.AnsweredSince(ctx, me.Handle, at[2], through[2])); got != second.Ref {
		t.Errorf("since the last answer's instant = %s, want %s", got, second.Ref)
	}
	if got := list(e.S.AnsweredSince(ctx, me.Handle, at[0], through[0])); got != first.Ref {
		t.Errorf("since the first answer, up to it = %s, want %s", got, first.Ref)
	}
	if got := list(e.S.AnsweredSince(ctx, me.Handle, t0, through[2])); got != first.Ref+","+second.Ref {
		t.Errorf("since the start = %s, want both of mine", got)
	}
}
