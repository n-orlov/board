package attention

import (
	"context"
	"sort"
	"time"

	"github.com/n-orlov/board/internal/domain"
)

// Reason is why a task needs a person.
type Reason string

// The reasons a task can carry, in the order TaskReasons lists them.
const (
	// ReasonStranded: the task's newest lease (by started_at, then lease id)
	// ran out and nobody acknowledged it.
	ReasonStranded Reason = "stranded"
	// ReasonAsking: the task has an open ask.
	ReasonAsking Reason = "asking"
	// ReasonDone: the task is done and not closed.
	ReasonDone Reason = "done"
)

// TaskReasons is what one task needs at the instant of the derivation. A closed
// task has none, and a task with none is not listed. Stranded, Asks and Done
// hold the rows behind the matching reasons; Blocking and AskerWaiting are true
// when any ask of the task is blocking or has its asker waiting.
type TaskReasons struct {
	Task         string
	Reasons      []Reason
	Blocking     bool
	AskerWaiting bool
	Stranded     *ExpiredLease
	Asks         []OpenAsk
	Done         *DoneTask
}

// Has reports whether r is one of the task's reasons.
func (t TaskReasons) Has(r Reason) bool {
	for _, have := range t.Reasons {
		if have == r {
			return true
		}
	}
	return false
}

// Derivation is the answer of Reasons: the reasons of every task that has one,
// ordered by initiative slug then task number, and the rows they come from in
// each section's own order. OpenAsks also holds the asks on no task, which have
// no task to carry a reason.
type Derivation struct {
	Tasks         []TaskReasons
	OpenAsks      []OpenAsk
	DoneNotClosed []DoneTask
	ExpiredLeases []ExpiredLease
}

// Of returns the reasons of task, none when it needs nothing.
func (d Derivation) Of(task string) TaskReasons {
	for _, t := range d.Tasks {
		if t.Task == task {
			return t
		}
	}
	return TaskReasons{Task: task}
}

// Reasons derives the reasons of the tasks the query keeps, judged at one instant
// after the leases that ran out are expired. It is the one place the stranded,
// asking and done states of a task are decided: the open_asks, done_not_closed
// and expired_leases sections of Get are the rows it returns.
func (s Store) Reasons(ctx context.Context, q Query) (Derivation, error) {
	sc, now, err := s.begin(ctx, q)
	if err != nil {
		return Derivation{}, err
	}
	return s.derive(ctx, sc, now)
}

func (s Store) derive(ctx context.Context, sc scope, now time.Time) (d Derivation, err error) {
	if d.OpenAsks, err = s.openAsks(ctx, sc, now); err != nil {
		return Derivation{}, err
	}
	if d.DoneNotClosed, err = s.doneNotClosed(ctx, sc); err != nil {
		return Derivation{}, err
	}
	if d.ExpiredLeases, err = s.expiredLeases(ctx, sc); err != nil {
		return Derivation{}, err
	}
	d.Tasks = reasonsOf(d)
	return d, nil
}

// reasonsOf groups the rows of d by task, in reason order.
func reasonsOf(d Derivation) []TaskReasons {
	g := grouping{}
	for i := range d.ExpiredLeases {
		g.of(d.ExpiredLeases[i].Task).Stranded = &d.ExpiredLeases[i]
	}
	for _, k := range d.OpenAsks {
		g.addAsk(k)
	}
	for i := range d.DoneNotClosed {
		g.of(d.DoneNotClosed[i].Task).Done = &d.DoneNotClosed[i]
	}
	out := make([]TaskReasons, 0, len(g))
	for _, t := range g {
		t.Reasons = reasonList(t)
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return refLess(out[i].Task, out[j].Task) })
	return out
}

// grouping holds one TaskReasons per task ref while reasonsOf collects rows.
type grouping map[string]*TaskReasons

// of returns the entry for task, creating it on first use.
func (g grouping) of(task string) *TaskReasons {
	if g[task] == nil {
		g[task] = &TaskReasons{Task: task}
	}
	return g[task]
}

// addAsk files an open ask under its task; an ask on an initiative has none and is skipped.
func (g grouping) addAsk(k OpenAsk) {
	if k.Task == "" {
		return
	}
	t := g.of(k.Task)
	t.Asks = append(t.Asks, k)
	t.Blocking = t.Blocking || k.Blocking
	t.AskerWaiting = t.AskerWaiting || k.AskerWaiting
}

// reasonList names t's reasons in order: stranded, asking, done.
func reasonList(t *TaskReasons) []Reason {
	var r []Reason
	if t.Stranded != nil {
		r = append(r, ReasonStranded)
	}
	if len(t.Asks) > 0 {
		r = append(r, ReasonAsking)
	}
	if t.Done != nil {
		r = append(r, ReasonDone)
	}
	return r
}

// refLess orders task refs by initiative slug, then task number.
func refLess(a, b string) bool {
	x, _ := domain.ParseTaskRef(a)
	y, _ := domain.ParseTaskRef(b)
	if x.Slug != y.Slug {
		return x.Slug < y.Slug
	}
	return x.Seq < y.Seq
}
