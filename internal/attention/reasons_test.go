package attention

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/tasks"
)

func (e env) reasons(t *testing.T, q Query) Derivation {
	t.Helper()
	d, err := e.A.Reasons(ctx, q)
	if err != nil {
		t.Fatalf("reasons %+v: %v", q, err)
	}
	return d
}

func (e env) newTask(t *testing.T, initiative string) string {
	t.Helper()
	return must(e.Tasks.Create(ctx, op, "key:x", tasks.New{Initiative: initiative, Title: "extra"})).Ref
}

func TestAClosedTaskHasNoReasonsWhateverItLeftBehind(t *testing.T) {
	e := newEnv(t)
	e.lease(t, me, "aa-1")
	e.ask(t, me, asks.New{Task: "aa-1", Question: "which db?", Blocking: true})
	e.tick(2 * time.Minute)
	if got := e.reasons(t, Query{}).Of("aa-1").Reasons; !reflect.DeepEqual(got, []Reason{ReasonStranded, ReasonAsking}) {
		t.Fatalf("before closing, reasons = %v, want stranded and asking", got)
	}
	must(e.Tasks.Close(ctx, op, "key:x", "aa-1", "dropped"))

	d := e.reasons(t, Query{})
	if got := d.Of("aa-1"); got.Reasons != nil || got.Blocking || got.AskerWaiting {
		t.Errorf("closed task reasons = %+v, want none", got)
	}
	if len(d.Tasks) != 0 || len(d.OpenAsks) != 0 || len(d.ExpiredLeases) != 0 || len(d.DoneNotClosed) != 0 {
		t.Errorf("a closed task is still listed: %+v", d)
	}
	if c := e.counts(t); c != (Counts{}) {
		t.Errorf("counts = %+v, want all zero", c)
	}
}

func TestADoneTaskWhoseNewestLeaseExpiredUnacknowledgedIsStrandedAndDone(t *testing.T) {
	e := newEnv(t)
	l := e.lease(t, me, "aa-1")
	e.tick(2 * time.Minute)
	must(e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}))

	got := e.reasons(t, Query{}).Of("aa-1")
	if !reflect.DeepEqual(got.Reasons, []Reason{ReasonStranded, ReasonDone}) {
		t.Fatalf("reasons = %v, want stranded then done", got.Reasons)
	}
	if got.Stranded == nil || got.Stranded.Lease != l.Ref || got.Done == nil || got.Done.Task != "aa-1" {
		t.Errorf("rows = stranded %+v, done %+v", got.Stranded, got.Done)
	}
	if !got.Has(ReasonStranded) || !got.Has(ReasonDone) || got.Has(ReasonAsking) {
		t.Errorf("Has disagrees with Reasons %v", got.Reasons)
	}

	must(e.Leases.Ack(ctx, op, "oidc:n", l.Ref))
	if got := e.reasons(t, Query{}).Of("aa-1").Reasons; !reflect.DeepEqual(got, []Reason{ReasonDone}) {
		t.Errorf("after the ack reasons = %v, want done only", got)
	}
}

func TestALaterLeaseClearsStranded(t *testing.T) {
	e := newEnv(t)
	e.lease(t, me, "aa-1")
	e.tick(2 * time.Minute)
	if got := e.reasons(t, Query{}).Of("aa-1").Reasons; !reflect.DeepEqual(got, []Reason{ReasonStranded}) {
		t.Fatalf("reasons = %v, want stranded", got)
	}
	newer := e.lease(t, bob, "aa-1")
	got := e.reasons(t, Query{}).Of("aa-1")
	if got.Reasons != nil {
		t.Errorf("with a live later lease reasons = %v, want none", got.Reasons)
	}
	e.tick(2 * time.Minute)
	got = e.reasons(t, Query{}).Of("aa-1")
	if !reflect.DeepEqual(got.Reasons, []Reason{ReasonStranded}) || got.Stranded.Lease != newer.Ref {
		t.Errorf("when the later lease expires too, reasons = %v on %+v, want stranded on %s", got.Reasons, got.Stranded, newer.Ref)
	}
}

func TestABlockingAskSetsBlockingAndAPolledAskSetsAskerWaiting(t *testing.T) {
	e := newEnv(t)
	plain := e.ask(t, me, asks.New{Task: "aa-1", Question: "ok?"})
	if got := e.reasons(t, Query{}).Of("aa-1"); !reflect.DeepEqual(got.Reasons, []Reason{ReasonAsking}) || got.Blocking || got.AskerWaiting {
		t.Fatalf("a plain ask gave %+v, want asking without flags", got)
	}
	blocking := e.ask(t, bob, asks.New{Task: "aa-1", Question: "which db?", Mode: "poll", Blocking: true})
	if err := e.Asks.Touch(ctx, blocking.ID); err != nil {
		t.Fatal(err)
	}
	e.ask(t, me, asks.New{Task: "aa-2", Question: "later?", Mode: "poll"})

	d := e.reasons(t, Query{})
	if got := d.Of("aa-1"); !got.Blocking || !got.AskerWaiting || len(got.Asks) != 2 {
		t.Errorf("aa-1 = %+v, want blocking and asker waiting over two asks", got)
	}
	if got := d.Of("aa-2"); got.Blocking || got.AskerWaiting || !got.Has(ReasonAsking) {
		t.Errorf("aa-2 = %+v, want asking with neither flag", got)
	}
	must(e.Asks.Withdraw(ctx, me, "key:x", plain.Ref, "no longer needed"))
	must(e.Asks.Withdraw(ctx, bob, "key:x", blocking.Ref, "no longer needed"))
	if got := e.reasons(t, Query{}).Of("aa-1"); got.Reasons != nil || got.Blocking {
		t.Errorf("with both asks withdrawn aa-1 = %+v, want none", got)
	}
}

func names[T any](items []T, task func(T) string) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, task(it))
	}
	sort.Strings(out)
	return out
}

func withReason(d Derivation, r Reason) []string {
	out := []string{}
	for _, tr := range d.Tasks {
		if tr.Has(r) {
			out = append(out, tr.Task)
		}
	}
	sort.Strings(out)
	return out
}

func TestTheSectionsAndTheTaskReasonsNameTheSameTasks(t *testing.T) {
	e := newEnv(t) // aa-1, aa-2, bb-1
	stranded := "aa-1"
	e.lease(t, me, stranded)
	asking := "aa-2"
	e.ask(t, me, asks.New{Task: asking, Question: "which db?", Blocking: true})
	e.ask(t, bob, asks.New{Initiative: "bb", Question: "on no task"})
	done := "bb-1"
	must(e.Tasks.Done(ctx, me, "key:x", done, tasks.DoneOptions{}))
	closed := e.newTask(t, "aa")
	e.lease(t, bob, closed)
	e.ask(t, bob, asks.New{Task: closed, Question: "moot?"})
	e.newTask(t, "bb") // quiet
	e.tick(2 * time.Minute)
	must(e.Tasks.Close(ctx, op, "key:x", closed, "dropped"))

	d := e.reasons(t, Query{})
	want := map[string][]Reason{stranded: {ReasonStranded}, asking: {ReasonAsking}, done: {ReasonDone}}
	got := map[string][]Reason{}
	for _, tr := range d.Tasks {
		got[tr.Task] = tr.Reasons
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("per-task reasons = %v, want %v (closed and quiet tasks have none)", got, want)
	}
	for _, none := range []string{closed, "bb-2", "bb-3"} {
		if r := d.Of(none).Reasons; r != nil {
			t.Errorf("%s has reasons %v", none, r)
		}
	}

	r := e.get(t, Query{})
	boundAsks := names(r.OpenAsks, func(k OpenAsk) string { return k.Task })
	for i, task := range boundAsks {
		if task == "" { // the ask on no task has no reason to carry
			boundAsks = append(boundAsks[:i], boundAsks[i+1:]...)
			break
		}
	}
	for _, c := range []struct {
		reason  Reason
		section []string
	}{
		{ReasonAsking, boundAsks},
		{ReasonDone, names(r.DoneNotClosed, func(d DoneTask) string { return d.Task })},
		{ReasonStranded, names(r.ExpiredLeases, func(l ExpiredLease) string { return l.Task })},
	} {
		if has := withReason(d, c.reason); !reflect.DeepEqual(has, c.section) {
			t.Errorf("%s: tasks with the reason %v, section names %v", c.reason, has, c.section)
		}
	}
	if len(r.OpenAsks) != 2 || r.Counts.OpenAsks != 2 || r.Counts.DoneNotClosed != 1 || r.Counts.ExpiredLeases != 1 {
		t.Errorf("sections = %d asks, counts %+v, want the ask on no task kept and counts 2, 1, 1", len(r.OpenAsks), r.Counts)
	}
}

func TestTasksWithReasonsComeInInitiativeThenNumberOrder(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 9; i++ {
		e.newTask(t, "aa") // aa-4 .. aa-12
	}
	for _, task := range []string{"bb-1", "aa-10", "aa-2", "aa-9"} {
		e.ask(t, me, asks.New{Task: task, Question: "q?"})
	}
	var got []string
	for _, tr := range e.reasons(t, Query{}).Tasks {
		got = append(got, tr.Task)
	}
	if want := []string{"aa-2", "aa-9", "aa-10", "bb-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestReasonsHonoursTheQueryAndRefusesABadOne(t *testing.T) {
	e := newEnv(t)
	e.ask(t, me, asks.New{Task: "aa-1", Question: "q?"})
	e.ask(t, me, asks.New{Task: "bb-1", Question: "q?"})
	if d := e.reasons(t, Query{Initiatives: []string{"bb"}}); len(d.Tasks) != 1 || d.Tasks[0].Task != "bb-1" {
		t.Errorf("reasons of bb = %+v, want bb-1 only", d.Tasks)
	}
	if _, err := e.A.Reasons(ctx, Query{Initiatives: []string{"zz"}}); err == nil {
		t.Error("an unknown initiative was accepted")
	}
}

// A lease number and a lease id can disagree on which of two leases started at
// the same instant is newer: the newest lease is the one with the larger id.
func TestTheNewestLeaseOnAStartedAtTieIsTheOneWithTheLargerID(t *testing.T) {
	e := newEnv(t)
	stale := e.lease(t, me, "aa-1")
	e.tick(2 * time.Minute)
	e.reasons(t, Query{}) // expires stale
	ended := e.lease(t, bob, "aa-1")
	must(e.Leases.End(ctx, bob, "key:x", ended.Ref, "finished"))
	db := e.A.Store.Writer()
	if _, err := db.ExecContext(ctx, `UPDATE leases SET started_at = (SELECT started_at FROM leases WHERE id = ?) WHERE id = ?`, stale.ID, ended.ID); err != nil {
		t.Fatal(err)
	}
	setID := func(from, to string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE leases SET id = ? WHERE id = ?`, to, from); err != nil {
			t.Fatal(err)
		}
	}
	if stale.N >= ended.N || stale.ID >= ended.ID {
		t.Fatalf("setup: stale %d/%s should precede ended %d/%s in both number and id", stale.N, stale.ID, ended.N, ended.ID)
	}
	if got := e.reasons(t, Query{}).Of("aa-1"); got.Reasons != nil {
		t.Fatalf("with the ended lease's id larger, reasons = %v, want none", got.Reasons)
	}

	high := "ZZZZZZZZZZZZZZZZZZZZZZZZZZ" // the stale lease's id now sorts after the ended one's, its number still before
	setID(stale.ID, high)
	got := e.reasons(t, Query{}).Of("aa-1")
	if !reflect.DeepEqual(got.Reasons, []Reason{ReasonStranded}) || got.Stranded == nil || got.Stranded.Lease != stale.Ref {
		t.Fatalf("with the stale lease's id larger, reasons = %v on %+v, want stranded on %s", got.Reasons, got.Stranded, stale.Ref)
	}
	if r := e.get(t, Query{}); len(r.ExpiredLeases) != 1 || r.ExpiredLeases[0].Lease != stale.Ref {
		t.Errorf("expired_leases = %+v, want %s alone", r.ExpiredLeases, stale.Ref)
	}

	setID(high, "00000000000000000000000000") // and back before the ended one's
	if got := e.reasons(t, Query{}).Of("aa-1"); got.Reasons != nil {
		t.Errorf("with the stale lease's id smaller again, reasons = %v, want none", got.Reasons)
	}
}
