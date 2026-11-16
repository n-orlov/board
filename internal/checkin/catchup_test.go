package checkin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/cursors"
	"github.com/n-orlov/village-hall/internal/notes"
)

func (e env) ask(t *testing.T, who string, q string) asks.Ask {
	t.Helper()
	a := me
	if who == "bob" {
		a = bob
	}
	k, err := e.Asks.Raise(ctx, a, "k", asks.New{Task: "aa-1", Question: q})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func (e env) answer(t *testing.T, ref string) {
	t.Helper()
	e.advance(time.Second)
	if _, err := e.Asks.Answer(ctx, bob, "k", ref, asks.Reply{Answer: "yes " + ref}); err != nil {
		t.Fatal(err)
	}
}

func (e env) catchup(t *testing.T) CatchupResult {
	t.Helper()
	r, err := e.S.Catchup(ctx, me, "k")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func askRefs(items []asks.Ask) string {
	out := make([]string, len(items))
	for i, k := range items {
		out[i] = k.Ref
	}
	return strings.Join(out, ",")
}

func TestACatchupWithNothingNewReturnsEmptyListsAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	n := e.eventCount(t)
	r := e.catchup(t)
	if r.Notes == nil || r.Answers == nil || len(r.Notes) != 0 || len(r.Answers) != 0 || r.NotesMore != 0 {
		t.Errorf("catch-up of nothing = %+v, want empty non-nil lists", r)
	}
	if got := e.eventCount(t); got != n {
		t.Errorf("events = %d, were %d: a catch-up that returned nothing wrote an event", got, n)
	}
	if pos, _ := e.Cur.Get(ctx, me.Handle, cursors.StreamNotes); pos != "" {
		t.Errorf("cursor = %q after an empty catch-up, want none", pos)
	}
}

func TestACatchupWithNotesOnlyAcknowledgesThemAndWritesOneEvent(t *testing.T) {
	e := newEnv(t)
	e.note(t, bob, notes.New{Summary: "one"})
	last := e.note(t, bob, notes.New{Task: "aa-2", Summary: "two"})
	n := e.eventCount(t)
	r := e.catchup(t)
	if noteRefs(r.Notes) != "N1,N2" || len(r.Answers) != 0 || r.NotesMore != 0 {
		t.Fatalf("catch-up = %+v, want N1 and N2 (every note, not only those that concern me) and no answers", r)
	}
	if r.Notes[0].HasBody || r.Notes[0].Body != "" {
		t.Errorf("a catch-up note carries a body: %+v", r.Notes[0])
	}
	if pos, _ := e.Cur.Get(ctx, me.Handle, cursors.StreamNotes); pos != last.ID {
		t.Errorf("cursor = %s, want the last note %s", pos, last.ID)
	}
	if got := e.eventTypes(t, n); got != "catchup" {
		t.Errorf("events = %s, want one catchup", got)
	}
	if again := e.catchup(t); len(again.Notes) != 0 || len(again.Answers) != 0 {
		t.Errorf("a second catch-up returned %+v", again)
	}
	if got := e.eventCount(t); got != n+1 {
		t.Errorf("events = %d, want %d after a second, empty catch-up", got, n+1)
	}
}

func TestACatchupWithAnswersOnlyReturnsMineOnceAndLeavesTheCursor(t *testing.T) {
	e := newEnv(t)
	mine := e.ask(t, "me", "mine")
	theirs := e.ask(t, "bob", "theirs")
	open := e.ask(t, "me", "unanswered")
	e.answer(t, mine.Ref)
	e.answer(t, theirs.Ref)
	n := e.eventCount(t)
	r := e.catchup(t)
	if askRefs(r.Answers) != mine.Ref || r.Answers[0].Answer != "yes "+mine.Ref || len(r.Notes) != 0 {
		t.Fatalf("catch-up = %+v, want only the answer to %s", r, mine.Ref)
	}
	if pos, _ := e.Cur.Get(ctx, me.Handle, cursors.StreamNotes); pos != "" {
		t.Errorf("cursor = %q: answers alone must not move it", pos)
	}
	if got := e.eventTypes(t, n); got != "catchup" {
		t.Errorf("events = %s, want one catchup", got)
	}
	if again := e.catchup(t); len(again.Answers) != 0 {
		t.Errorf("the same answer came back: %+v", again)
	}
	e.answer(t, open.Ref)
	if next := e.catchup(t); askRefs(next.Answers) != open.Ref {
		t.Errorf("answers after the previous catch-up = %s, want %s", askRefs(next.Answers), open.Ref)
	}
}

func TestACatchupWithBothReturnsBothInOneEventWhoseDataSaysSo(t *testing.T) {
	e := newEnv(t)
	e.note(t, bob, notes.New{Summary: "one"})
	k := e.ask(t, "me", "q")
	e.answer(t, k.Ref)
	n := e.eventCount(t)
	r := e.catchup(t)
	if noteRefs(r.Notes) != "N1" || askRefs(r.Answers) != k.Ref {
		t.Fatalf("catch-up = %+v, want N1 and the answer", r)
	}
	if got := e.eventTypes(t, n); got != "catchup" {
		t.Fatalf("events = %s, want one catchup", got)
	}
	var summary, data string
	if err := e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT summary, data FROM events WHERE type = 'catchup'`).Scan(&summary, &data); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		t.Fatal(err)
	}
	if summary != "catch-up: 1 notes, 1 answers" || d["notes"] != float64(1) || d["answers"] != float64(1) || d["answers_through"] == float64(0) {
		t.Errorf("event = %q %s", summary, data)
	}
}

func TestTheFirstCatchupOnlyReachesBackAnAnswerWindow(t *testing.T) {
	e := newEnv(t)
	old := e.ask(t, "me", "old")
	e.answer(t, old.Ref)
	e.advance(AnswerWindow + time.Minute)
	fresh := e.ask(t, "me", "fresh")
	e.answer(t, fresh.Ref)
	if r := e.catchup(t); askRefs(r.Answers) != fresh.Ref {
		t.Errorf("first catch-up answers = %s, want only %s", askRefs(r.Answers), fresh.Ref)
	}
}

func TestACatchupAcknowledgesAtMostItsLimitAndCountsTheRest(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < CatchupNotesLimit+2; i++ {
		e.note(t, bob, notes.New{Summary: "b"})
	}
	r := e.catchup(t)
	if len(r.Notes) != CatchupNotesLimit || r.NotesMore != 2 || r.Notes[0].Ref != "N1" {
		t.Fatalf("catch-up = %d notes (%d more) from %s, want %d and 2 more", len(r.Notes), r.NotesMore, r.Notes[0].Ref, CatchupNotesLimit)
	}
	rest := e.catchup(t)
	if noteRefs(rest.Notes) != "N101,N102" || rest.NotesMore != 0 {
		t.Errorf("next catch-up = %s (%d more), want N101,N102", noteRefs(rest.Notes), rest.NotesMore)
	}
}

func TestACatchupOnAnUnreadableDatabaseFailsAndReturnsNothing(t *testing.T) {
	e := newEnv(t)
	e.note(t, bob, notes.New{Summary: "one"})
	if err := e.S.Cursors.Store.Close(); err != nil {
		t.Fatal(err)
	}
	if r, err := e.S.Catchup(ctx, me, "k"); err == nil || len(r.Notes) != 0 || len(r.Answers) != 0 {
		t.Errorf("Catchup on a closed database = %+v, %v; want an error and nothing", r, err)
	}
}
