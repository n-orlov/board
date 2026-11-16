package checkin

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// finishState is the events, notes and actors counts and the status of aa-1, which a
// refused finish must leave as they were.
func (e env) finishState(t *testing.T) string {
	t.Helper()
	var got string
	err := e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM notes) || '/' ||
		(SELECT count(*) FROM leases WHERE state = 'live') || '/' || (SELECT count(*) FROM actors) || '/' ||
		(SELECT status FROM tasks WHERE seq = 1)`).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (e env) eventTypes(t *testing.T, since int) string {
	t.Helper()
	rows, err := e.S.Leases.Store.Reader().QueryContext(ctx, `SELECT type FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var types []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		types = append(types, s)
	}
	return strings.Join(types[since:], ",")
}

func (e env) eventCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.S.Leases.Store.Reader().QueryRowContext(ctx, `SELECT count(*) FROM events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAFinishOnAGivenTaskEndsTheLeaseAndWritesTheNoteWithTwoEvents(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
	n := e.eventCount(t)
	e.advance(time.Minute)
	r, err := e.S.Finish(ctx, me, "k", FinishRequest{Task: "aa-1", Outcome: "  shipped it "})
	if err != nil {
		t.Fatal(err)
	}
	if r.Done || r.Lease.Ref != "L1" || r.Lease.State != "ended" || r.Lease.Outcome != "shipped it" ||
		r.Note.Summary != "shipped it" || r.Note.Task != "aa-1" || r.Note.Actor != me.Handle {
		t.Errorf("finish = %+v", r)
	}
	if got := e.eventTypes(t, n); got != "lease_ended,note_posted" {
		t.Errorf("events = %s", got)
	}
	if got := e.finishState(t); !strings.HasSuffix(got, "/0/1/open") {
		t.Errorf("state = %s, want no live lease and an open task", got)
	}
}

func TestAFinishWithDoneAlsoMarksTheTaskDoneWithThreeEvents(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "work"})
	n := e.eventCount(t)
	r, err := e.S.Finish(ctx, me, "k", FinishRequest{Task: "aa-1", Outcome: "all of it", Done: true})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Done || r.Lease.Outcome != "all of it" || r.Lease.EndedBy != "" {
		t.Errorf("finish = %+v", r)
	}
	if got := e.eventTypes(t, n); got != "lease_ended,task_done,note_posted" {
		t.Errorf("events = %s", got)
	}
	if got := e.finishState(t); !strings.HasSuffix(got, "/0/1/done") {
		t.Errorf("state = %s, want a done task", got)
	}
}

func TestAFinishWithoutATaskUsesTheOnlyLiveLease(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-2", Intent: "work"})
	r, err := e.S.Finish(ctx, me, "k", FinishRequest{Outcome: "done here"})
	if err != nil || r.Lease.Task != "aa-2" || r.Note.Task != "aa-2" {
		t.Errorf("finish = %+v, %v", r, err)
	}
}

func TestAFinishWithoutATaskAndWithNoOrSeveralLeasesIsAConflictThatWritesNothing(t *testing.T) {
	e := newEnv(t)
	_, err := e.S.Finish(ctx, me, "k", FinishRequest{Outcome: "x"})
	var c leases.ConflictError
	if !errors.As(err, &c) || c.Msg != "you hold no live lease" || c.Next == "" {
		t.Errorf("no lease: %v", err)
	}
	e.start(t, StartRequest{Task: "aa-1", Intent: "one"})
	e.start(t, StartRequest{Task: "aa-2", Intent: "two"})
	before := e.finishState(t)
	_, err = e.S.Finish(ctx, me, "k", FinishRequest{Outcome: "x"})
	if !errors.As(err, &c) || c.Msg != "you hold 2 live leases" || !strings.Contains(c.Next, "aa-1 (L1), aa-2 (L2)") {
		t.Errorf("two leases: %v, next %q", err, c.Next)
	}
	if got := e.finishState(t); got != before {
		t.Errorf("state = %s after refusals, were %s", got, before)
	}
}

func TestAFinishOnATaskTheCallerHoldsNoLeaseOnIsAConflictThatWritesNothing(t *testing.T) {
	e := newEnv(t)
	if _, err := e.S.Leases.Start(ctx, bob, "k", leases.Start{Task: "aa-1", Intent: "theirs"}); err != nil {
		t.Fatal(err)
	}
	e.start(t, StartRequest{Task: "aa-2", Intent: "mine"})
	before := e.finishState(t)
	for _, done := range []bool{false, true} {
		_, err := e.S.Finish(ctx, me, "k", FinishRequest{Task: "aa-1", Outcome: "x", Done: done})
		var c leases.ConflictError
		if !errors.As(err, &c) || c.Msg != "you hold no live lease on task aa-1" || !strings.Contains(c.Next, "vh start aa-1") {
			t.Errorf("done=%v: %v", done, err)
		}
	}
	if got := e.finishState(t); got != before {
		t.Errorf("state = %s after refusals, were %s", got, before)
	}
}

func TestAFinishWithDoneWhileAnotherHoldsALiveLeaseChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "mine"})
	if _, err := e.S.Leases.Start(ctx, bob, "k", leases.Start{Task: "aa-1", Intent: "theirs"}); err != nil {
		t.Fatal(err)
	}
	before := e.finishState(t)
	_, err := e.S.Finish(ctx, me, "k", FinishRequest{Task: "aa-1", Outcome: "x", Done: true})
	var held tasks.LeaseHeldError
	if !errors.As(err, &held) || len(held.Holders) != 1 || held.Holders[0].Holder != bob.Handle {
		t.Errorf("err = %v, want the other holder named", err)
	}
	if got := e.finishState(t); got != before {
		t.Errorf("state = %s after the refusal, were %s: the lease end or the note was written", got, before)
	}
	if r, err := e.S.Finish(ctx, me, "k", FinishRequest{Task: "aa-1", Outcome: "x"}); err != nil || r.Done {
		t.Errorf("without done: %+v, %v", r, err)
	}
}

func TestAFinishRefusesAWrongOutcomeAndAnUnknownTaskBeforeWritingAnything(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "mine"})
	before := e.finishState(t)
	for name, c := range map[string]struct {
		in   FinishRequest
		want error
	}{
		"empty outcome":    {FinishRequest{Outcome: "  "}, leases.ErrInvalid},
		"long outcome":     {FinishRequest{Outcome: strings.Repeat("x", 201)}, leases.ErrInvalid},
		"two lines":        {FinishRequest{Outcome: "a\nb"}, leases.ErrInvalid},
		"unknown task":     {FinishRequest{Task: "aa-9", Outcome: "x"}, leases.ErrNotFound},
		"malformed ref":    {FinishRequest{Task: "!", Outcome: "x"}, leases.ErrNotFound},
		"unknown for done": {FinishRequest{Task: "aa-9", Outcome: "x", Done: true}, leases.ErrNotFound},
	} {
		if _, err := e.S.Finish(ctx, me, "k", c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	var bad leases.InvalidError
	if _, err := e.S.Finish(ctx, me, "k", FinishRequest{Outcome: ""}); !errors.As(err, &bad) || bad.Next == "" {
		t.Errorf("an empty outcome needs a next: %v", err)
	}
	if got := e.finishState(t); got != before {
		t.Errorf("state = %s after refusals, were %s", got, before)
	}
}

func TestAFinishDoesNotUseAnExpiredLease(t *testing.T) {
	e := newEnv(t)
	e.start(t, StartRequest{Task: "aa-1", Intent: "mine"})
	e.advance(3 * time.Hour)
	_, err := e.S.Finish(ctx, me, "k", FinishRequest{Task: "aa-1", Outcome: "x"})
	if !errors.Is(err, leases.ErrConflict) {
		t.Errorf("finish on an expired lease = %v, want a conflict", err)
	}
}
