package tasks

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
)

// fixed returns the store s with a clock that stands at *at until the test moves
// it.
func fixed(s Store, at *time.Time) Store {
	s.Now = func() time.Time { return *at }
	return s
}

func mkWatch(t *testing.T, s Store, cadence int64) Task {
	t.Helper()
	got, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "watch", Kind: domain.KindWatch, CadenceSeconds: &cadence})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCheckOnAWatchTaskRecordsAnEventAndSetsNextCheckAt(t *testing.T) {
	now := t0
	s := fixed(newStore(t), &now)
	initiative(t, s, "aa")
	w := mkWatch(t, s, 3600)
	now = t0.Add(10 * time.Minute)
	given := t0.Add(48 * time.Hour)

	got, err := s.Check(ctx, audit2("codex@host"), "key:check", w.Ref, "build still red", &given)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextCheckAt == nil || !got.NextCheckAt.Equal(given) || !got.UpdatedAt.Equal(now) {
		t.Errorf("next_check_at = %v updated_at = %v, want %v and %v", got.NextCheckAt, got.UpdatedAt, given, now)
	}
	ev := events(t, s, EventCheck)
	if len(ev) != 1 || ev[0].TaskID != w.ID || ev[0].InitiativeID != w.InitiativeID || ev[0].RefID != w.ID || ev[0].Principal != "key:check" ||
		ev[0].Summary != "check on aa-1: build still red" || ev[0].Data["ref"] != "aa-1" || ev[0].Data["summary"] != "build still red" ||
		ev[0].Data["next_check_at"] != given.Format(time.RFC3339Nano) {
		t.Fatalf("events = %+v", ev)
	}
	if n := count(t, s, `SELECT count(*) FROM actors WHERE handle = 'codex@host'`); n != 1 {
		t.Errorf("checking actor rows = %d, want 1", n)
	}
}

func TestCheckWithoutNextCheckAtUsesNowPlusCadence(t *testing.T) {
	now := t0
	s := fixed(newStore(t), &now)
	initiative(t, s, "aa")
	w := mkWatch(t, s, 3600)
	now = t0.Add(10 * time.Minute)
	got, err := s.Check(ctx, who, "key:c", w.Ref, "ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(time.Hour); got.NextCheckAt == nil || !got.NextCheckAt.Equal(want) {
		t.Fatalf("next_check_at = %v, want %v", got.NextCheckAt, want)
	}
	if ev := events(t, s, EventCheck); len(ev) != 1 || ev[0].Data["next_check_at"] != now.Add(time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("events = %+v", ev)
	}
}

func TestCheckRefusesAWorkTaskAndAClosedWatchWithoutWriting(t *testing.T) {
	now := t0
	s := fixed(newStore(t), &now)
	initiative(t, s, "aa")
	work := mk(t, s, "aa", "work")
	w := mkWatch(t, s, 3600)
	if _, err := s.Close(ctx, who, "key:c", w.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	before := []int{count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM actors`)}
	for _, tc := range []struct{ ref, want string }{
		{work.Ref, "cannot record a check on task aa-1: it is a work task"},
		{w.Ref, "cannot record a check on task aa-2: it is closed"},
	} {
		_, err := s.Check(ctx, audit2("new@host"), "key:c", tc.ref, "x", nil)
		var ce ConflictError
		if !errors.Is(err, ErrConflict) || !errors.As(err, &ce) || err.Error() != tc.want {
			t.Errorf("Check(%s) = %v, want a conflict %q", tc.ref, err, tc.want)
		}
	}
	after := []int{count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM actors`)}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("events and actors went from %v to %v", before, after)
	}
	got, _ := s.Get(ctx, work.Ref)
	if got.NextCheckAt != nil {
		t.Errorf("work task next_check_at = %v", got.NextCheckAt)
	}
}

func TestCheckAndMilestoneRefuseAnUnknownTaskAndABadSummary(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	w := mkWatch(t, s, 3600)
	if _, err := s.Check(ctx, who, "k", "aa-9", "x", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("check on an unknown task = %v, want ErrNotFound", err)
	}
	if _, err := s.Milestone(ctx, who, "k", "aa-9", "x", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("milestone on an unknown task = %v, want ErrNotFound", err)
	}
	before := count(t, s, `SELECT count(*) FROM events`)
	for _, tc := range []struct {
		name, summary string
		ok            bool
	}{
		{"empty", "", false}, {"blank", "  \n ", false}, {"1 char", "a", true},
		{"200 chars", strings.Repeat("a", 200), true}, {"201 chars", strings.Repeat("a", 201), false},
	} {
		_, cerr := s.Check(ctx, who, "k", w.Ref, tc.summary, nil)
		_, merr := s.Milestone(ctx, who, "k", w.Ref, tc.summary, nil)
		if (cerr == nil) != tc.ok || (merr == nil) != tc.ok {
			t.Errorf("%s: check %v, milestone %v, want ok=%v", tc.name, cerr, merr, tc.ok)
		}
		if !tc.ok && (!errors.Is(cerr, ErrInvalid) || !errors.Is(merr, ErrInvalid)) {
			t.Errorf("%s: check %v, milestone %v, want ErrInvalid", tc.name, cerr, merr)
		}
	}
	if got := count(t, s, `SELECT count(*) FROM events`); got != before+4 {
		t.Errorf("events = %d, want %d (two valid summaries, a check and a milestone each)", got, before+4)
	}
}

func TestMilestoneCarriesItsDataObjectAndLeavesTheTaskAlone(t *testing.T) {
	now := t0
	s := fixed(newStore(t), &now)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "work")
	now = t0.Add(time.Hour)
	data := map[string]any{"version": "1.2", "n": float64(3), "nested": map[string]any{"ok": true}}
	got, err := s.Milestone(ctx, audit2("codex@host"), "key:m", tk.Ref, "released\n1.2", data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(tk.UpdatedAt) {
		t.Errorf("milestone moved updated_at to %v", got.UpdatedAt)
	}
	ev := events(t, s, EventMilestone)
	if len(ev) != 1 || ev[0].TaskID != tk.ID || ev[0].Principal != "key:m" || ev[0].Summary != "milestone on aa-1: released 1.2" ||
		ev[0].Data["summary"] != "released\n1.2" || ev[0].Data["ref"] != "aa-1" || !reflect.DeepEqual(ev[0].Data["data"], any(data)) {
		t.Fatalf("events = %+v", ev)
	}
	if _, err := s.Milestone(ctx, who, "k", tk.Ref, "bare", nil); err != nil {
		t.Fatal(err)
	}
	if ev := events(t, s, EventMilestone); len(ev) != 2 || ev[1].Data["data"] != nil || len(ev[1].Data) != 2 {
		t.Errorf("a milestone without data carries %+v", ev[1].Data)
	}
}

func TestOverdueIsAnOpenOrDoneWatchTaskWithNextCheckAtBeforeNow(t *testing.T) {
	now := t0
	s := fixed(newStore(t), &now)
	initiative(t, s, "aa")
	overdue := mkWatch(t, s, 3600)
	closed := mkWatch(t, s, 3600)
	future := mkWatch(t, s, 7200)
	work := mk(t, s, "aa", "work")
	if _, err := s.Close(ctx, who, "k", closed.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(90 * time.Minute)
	for _, tc := range []struct {
		name string
		ref  string
		want bool
	}{{"open watch", overdue.Ref, true}, {"closed watch", closed.Ref, false}, {"future next_check_at", future.Ref, false}, {"work", work.Ref, false}} {
		got, err := s.Get(ctx, tc.ref)
		if err != nil || got.Overdue != tc.want {
			t.Errorf("%s: Overdue = %v (%v), want %v", tc.name, got.Overdue, err, tc.want)
		}
	}
	if _, err := s.Done(ctx, who, "k", overdue.Ref, DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, overdue.Ref); !got.Overdue {
		t.Error("a done watch task past its next_check_at is overdue")
	}
	if _, err := s.Check(ctx, who, "k", overdue.Ref, "checked", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, overdue.Ref); got.Overdue {
		t.Error("a check moves next_check_at into the future, so the task is no longer overdue")
	}
}

func TestMilestoneInIsTheCallersTransactionAndChecksTheSummaryFirst(t *testing.T) {
	now := t0
	s := fixed(newStore(t), &now)
	initiative(t, s, "aa")
	tk := mk(t, s, "aa", "work")
	write := func(commit bool, ref, summary string) error {
		tx, err := s.Store.Writer().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.MilestoneIn(ctx, tx, who, "k", ref, summary, nil)
		if err == nil && got.Ref != tk.Ref {
			t.Errorf("MilestoneIn answered %s, want %s", got.Ref, tk.Ref)
		}
		if commit && err == nil {
			return tx.Commit()
		}
		_ = tx.Rollback()
		return err
	}
	if err := write(false, tk.Ref, "rolled back"); err != nil || len(events(t, s, EventMilestone)) != 0 {
		t.Errorf("a rolled back milestone: %v, %d events", err, len(events(t, s, EventMilestone)))
	}
	if err := write(true, tk.Ref, "kept"); err != nil || len(events(t, s, EventMilestone)) != 1 {
		t.Errorf("a committed milestone: %v, %d events", err, len(events(t, s, EventMilestone)))
	}
	if err := write(true, "aa-9", "kept"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown task: %v, want ErrNotFound", err)
	}
	if err := write(true, "aa-9", " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("a blank summary on an unknown task: %v, want ErrInvalid before the task is read", err)
	}
}
