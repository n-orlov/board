package tasks

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
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/store"
)

var (
	t0  = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	who = audit.Actor{Handle: "claude@host"}
	ctx = context.Background()
)

// newStore is a Store over a migrated SQLite database whose clock advances one
// second per call, so ids sort in creation order.
func newStore(t *testing.T) Store {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	n := 0
	return Store{Store: st, Now: func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }}
}

func initiative(t *testing.T, s Store, slug string) initiatives.Initiative {
	t.Helper()
	i, err := initiatives.Store{Store: s.Store, Now: s.Now}.Create(ctx, who, "key:caller", initiatives.New{Slug: slug, Title: "I " + slug})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func mk(t *testing.T, s Store, slug, title string) Task {
	t.Helper()
	got, err := s.Create(ctx, who, "key:caller", New{Initiative: slug, Title: title})
	if err != nil {
		t.Fatalf("create %s %q: %v", slug, title, err)
	}
	return got
}

func count(t *testing.T, s Store, query string) int {
	t.Helper()
	var n int
	if err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(query)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type event struct {
	Type, TaskID, InitiativeID, RefID, Summary, Principal string
	Data                                                  map[string]any
}

func events(t *testing.T, s Store, typ string) []event {
	t.Helper()
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT type, COALESCE(task_id, ''), COALESCE(initiative_id, ''), COALESCE(ref_id, ''), summary, principal, data FROM events WHERE type = ? ORDER BY id`), typ)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []event
	for rows.Next() {
		var e event
		var data string
		if err := rows.Scan(&e.Type, &e.TaskID, &e.InitiativeID, &e.RefID, &e.Summary, &e.Principal, &data); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(data), &e.Data); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// Slugs have at least two characters (domain.ValidateSlug), so the refs
// a-1, a-2 and b-1 of the requirement are aa-1, aa-2 and bb-1 here.
func TestRefsCountPerInitiativeAndAreNeverReused(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	initiative(t, s, "bb")
	var refs []string
	for _, slug := range []string{"aa", "aa", "bb"} {
		refs = append(refs, mk(t, s, slug, "t").Ref)
	}
	if want := []string{"aa-1", "aa-2", "bb-1"}; !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
	// The highest seq is gone for good: even a deleted row does not hand it out again.
	if _, err := s.Store.Writer().ExecContext(ctx, `DELETE FROM tasks WHERE initiative_id = (SELECT id FROM initiatives WHERE slug = 'aa') AND seq = 2`); err != nil {
		t.Fatal(err)
	}
	if got := mk(t, s, "aa", "t").Ref; got != "aa-3" {
		t.Fatalf("ref after deleting aa-2 = %s, want aa-3", got)
	}
	if got := mk(t, s, "bb", "t").Ref; got != "bb-2" {
		t.Fatalf("ref = %s, want bb-2", got)
	}
}

func TestGetFindsATaskByRefOrULIDInEitherCase(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	made := mk(t, s, "AA", "one")
	if made.Ref != "aa-1" || made.Initiative != "aa" || made.Seq != 1 || made.CreatedBy != who.Handle {
		t.Fatalf("created %+v", made)
	}
	for _, ref := range []string{"aa-1", "AA-1", made.ID, strings.ToLower(made.ID)} {
		got, err := s.Get(ctx, ref)
		if err != nil || got.ID != made.ID {
			t.Errorf("Get(%q) = %s, %v", ref, got.ID, err)
		}
	}
	for _, ref := range []string{"aa-2", "bb-1", "aa-0", "aa", "", "01ARZ3NDEKTSV4RRFFQ69G5FAV"} {
		if _, err := s.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", ref, err)
		}
	}
}

func TestCreateStoresTheTaskAndOneTaskCreatedEvent(t *testing.T) {
	s := newStore(t)
	ini := initiative(t, s, "aa")
	start, end := t0.Add(24*time.Hour), t0.Add(48*time.Hour)
	refs := []domain.ExternalRef{{Label: "pr", URL: "https://example.com/1"}}
	got, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "  Title  ", Description: "d", PlannedStart: &start, PlannedEnd: &end, ExternalRefs: refs})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Title" || got.Description != "d" || got.Kind != "work" || got.Status != "open" || got.Resolution != "" ||
		got.CadenceSeconds != nil || got.NextCheckAt != nil || !reflect.DeepEqual(got.ExternalRefs, refs) ||
		!got.PlannedStart.Equal(start) || !got.PlannedEnd.Equal(end) || got.InitiativeID != ini.ID ||
		!got.UpdatedAt.Equal(got.CreatedAt) || got.DoneAt != nil || got.ClosedAt != nil || got.DoneBy != "" || got.ClosedBy != "" {
		t.Errorf("created %+v", got)
	}
	evs := events(t, s, EventCreated)
	if len(evs) != 1 {
		t.Fatalf("task_created events = %d, want 1", len(evs))
	}
	e := evs[0]
	if e.TaskID != got.ID || e.InitiativeID != ini.ID || e.RefID != got.ID || e.Principal != "key:caller" ||
		e.Summary != "task aa-1 created" || e.Data["ref"] != "aa-1" || e.Data["title"] != "Title" || e.Data["kind"] != "work" {
		t.Errorf("event %+v", e)
	}
}

func TestCreateAWatchTaskDefaultsTheNextCheckToNowPlusCadence(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	cadence := int64(3600)
	got, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "w", Kind: "watch", CadenceSeconds: &cadence})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "watch" || *got.CadenceSeconds != 3600 || !got.NextCheckAt.Equal(got.CreatedAt.Add(time.Hour)) {
		t.Errorf("watch task %+v next %v", got, got.NextCheckAt)
	}
	next := t0.Add(5 * time.Hour)
	got, err = s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "w", Kind: "watch", CadenceSeconds: &cadence, NextCheckAt: &next})
	if err != nil || !got.NextCheckAt.Equal(next) {
		t.Errorf("explicit next_check_at = %v, %v", got.NextCheckAt, err)
	}
}

func TestCreateRefusalsLeaveNothingBehind(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	closed := initiative(t, s, "cc")
	if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE initiatives SET status = 'closed' WHERE id = ?`, closed.ID); err != nil {
		t.Fatal(err)
	}
	cadence := int64(59)
	for _, tc := range []struct {
		name string
		in   New
		want error
	}{
		{"empty title", New{Initiative: "aa", Title: " "}, ErrInvalid},
		{"unknown kind", New{Initiative: "aa", Title: "t", Kind: "other"}, ErrInvalid},
		{"watch fields on work", New{Initiative: "aa", Title: "t", CadenceSeconds: &cadence}, ErrInvalid},
		{"cadence 59", New{Initiative: "aa", Title: "t", Kind: "watch", CadenceSeconds: &cadence}, ErrInvalid},
		{"watch without cadence", New{Initiative: "aa", Title: "t", Kind: "watch"}, ErrInvalid},
		{"bad url", New{Initiative: "aa", Title: "t", ExternalRefs: []domain.ExternalRef{{Label: "x", URL: "ftp://a.b"}}}, ErrInvalid},
		{"unknown initiative", New{Initiative: "zz", Title: "t"}, ErrNoInitiative},
		{"malformed initiative", New{Initiative: "Z!", Title: "t"}, ErrNoInitiative},
		{"closed initiative", New{Initiative: "cc", Title: "t"}, ErrInitiativeClosed},
	} {
		before := []int{count(t, s, `SELECT count(*) FROM tasks`), count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM actors`)}
		if _, err := s.Create(ctx, audit.Actor{Handle: "other@host"}, "key:caller", tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		after := []int{count(t, s, `SELECT count(*) FROM tasks`), count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM actors`)}
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s changed tasks, events, actors from %v to %v", tc.name, before, after)
		}
	}
	// A refused create does not spend a seq either.
	if got := mk(t, s, "aa", "t").Ref; got != "aa-1" {
		t.Errorf("first ref after refusals = %s", got)
	}
}

func TestPatchWritesTaskUpdatedNamingTheChangedFields(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	made := mk(t, s, "aa", "old")
	start, end := t0.Add(time.Hour), t0.Add(2*time.Hour)
	refs := []domain.ExternalRef{{Label: "ci", URL: "http://example.com/b/1"}}
	got, err := s.Patch(ctx, audit.Actor{Handle: "other@host"}, "key:other", "AA-1", Patch{
		Title:        Field[string]{Set: true, Value: " new "},
		Description:  Field[string]{Set: true, Value: "text"},
		PlannedStart: Field[time.Time]{Set: true, Value: start},
		PlannedEnd:   Field[time.Time]{Set: true, Value: end},
		ExternalRefs: Field[[]domain.ExternalRef]{Set: true, Value: refs},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "new" || got.Description != "text" || !got.PlannedStart.Equal(start) || !got.PlannedEnd.Equal(end) ||
		!reflect.DeepEqual(got.ExternalRefs, refs) || !got.UpdatedAt.After(made.UpdatedAt) || got.CreatedBy != who.Handle {
		t.Errorf("patched %+v", got)
	}
	evs := events(t, s, EventUpdated)
	if len(evs) != 1 {
		t.Fatalf("task_updated events = %d, want 1", len(evs))
	}
	e := evs[0]
	want := []any{"title", "description", "planned_start", "planned_end", "external_refs"}
	if !reflect.DeepEqual(e.Data["changed"], want) || e.TaskID != made.ID || e.Principal != "key:other" ||
		e.Summary != "task aa-1 updated: title, description, planned_start, planned_end, external_refs" {
		t.Errorf("event %+v", e)
	}
	if n := count(t, s, `SELECT count(*) FROM actors WHERE handle = 'other@host'`); n != 1 {
		t.Errorf("patching actor rows = %d, want 1", n)
	}
}

func TestPatchOnlyNamesFieldsWhoseValueChanged(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	made := mk(t, s, "aa", "same")
	got, err := s.Patch(ctx, who, "key:caller", made.ID, Patch{
		Title:       Field[string]{Set: true, Value: "same"},
		Description: Field[string]{Set: true, Value: "other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "other" {
		t.Errorf("description = %q", got.Description)
	}
	if e := events(t, s, EventUpdated); len(e) != 1 || !reflect.DeepEqual(e[0].Data["changed"], []any{"description"}) {
		t.Errorf("events %+v", e)
	}
}

func TestANoOpPatchWritesNoEventAndNoActor(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	start := t0.Add(time.Hour)
	made, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "t", PlannedStart: &start,
		ExternalRefs: []domain.ExternalRef{{Label: "x", URL: "http://a.b"}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]Patch{
		"empty":          {},
		"same title":     {Title: Field[string]{Set: true, Value: " t "}},
		"same date":      {PlannedStart: Field[time.Time]{Set: true, Value: start.Add(300 * time.Microsecond)}},
		"clear no date":  {PlannedEnd: Field[time.Time]{Set: true, Null: true}},
		"same refs":      {ExternalRefs: Field[[]domain.ExternalRef]{Set: true, Value: []domain.ExternalRef{{Label: "x", URL: "http://a.b"}}}},
		"same empty doc": {Description: Field[string]{Set: true, Value: ""}},
	} {
		events0, actors0 := count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM actors`)
		got, err := s.Patch(ctx, audit.Actor{Handle: "stranger@host"}, "key:s", made.Ref, p)
		if err != nil || !reflect.DeepEqual(got, made) {
			t.Errorf("%s: %+v, %v; want the task unchanged", name, got, err)
		}
		if n := count(t, s, `SELECT count(*) FROM events`); n != events0 {
			t.Errorf("%s wrote %d events", name, n-events0)
		}
		if n := count(t, s, `SELECT count(*) FROM actors`); n != actors0 {
			t.Errorf("%s wrote an actor", name)
		}
	}
	if n := len(events(t, s, EventUpdated)); n != 0 {
		t.Errorf("task_updated events = %d, want 0", n)
	}
}

func TestPatchClearsOptionalFields(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	start, end := t0.Add(time.Hour), t0.Add(2*time.Hour)
	made, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "t", PlannedStart: &start, PlannedEnd: &end})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Patch(ctx, who, "key:caller", made.Ref, Patch{
		PlannedStart: Field[time.Time]{Set: true, Null: true}, PlannedEnd: Field[time.Time]{Set: true, Null: true}})
	if err != nil || got.PlannedStart != nil || got.PlannedEnd != nil {
		t.Fatalf("cleared dates: %+v, %v", got, err)
	}
	if e := events(t, s, EventUpdated); len(e) != 1 || !reflect.DeepEqual(e[0].Data["changed"], []any{"planned_start", "planned_end"}) {
		t.Errorf("events %+v", e)
	}
}

func TestPatchWatchFields(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	cadence := int64(120)
	w, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "w", Kind: "watch", CadenceSeconds: &cadence})
	if err != nil {
		t.Fatal(err)
	}
	next := t0.Add(9 * time.Hour)
	got, err := s.Patch(ctx, who, "key:caller", w.Ref, Patch{
		CadenceSeconds: Field[int64]{Set: true, Value: 300}, NextCheckAt: Field[time.Time]{Set: true, Value: next}})
	if err != nil || *got.CadenceSeconds != 300 || !got.NextCheckAt.Equal(next) {
		t.Fatalf("watch patch: %+v, %v", got, err)
	}
	if e := events(t, s, EventUpdated); len(e) != 1 || !reflect.DeepEqual(e[0].Data["changed"], []any{"cadence_seconds", "next_check_at"}) {
		t.Errorf("events %+v", e)
	}
}

func TestPatchRefusalsChangeNothing(t *testing.T) {
	s := newStore(t)
	ini := initiative(t, s, "aa")
	work := mk(t, s, "aa", "work")
	closed := mk(t, s, "aa", "closed")
	if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE tasks SET status = 'closed', resolution = 'dropped' WHERE id = ?`, closed.ID); err != nil {
		t.Fatal(err)
	}
	late := mk(t, s, "aa", "late")
	cad := int64(59)
	title := Field[string]{Set: true, Value: "x"}
	for _, tc := range []struct {
		name string
		ref  string
		p    Patch
		want error
	}{
		{"unknown task", "aa-9", Patch{Title: title}, ErrNotFound},
		{"malformed ref", "aa", Patch{Title: title}, ErrNotFound},
		{"closed task", closed.Ref, Patch{Title: title}, ErrClosed},
		{"closed task no-op", closed.Ref, Patch{}, ErrClosed},
		{"empty title", work.Ref, Patch{Title: Field[string]{Set: true, Value: ""}}, ErrInvalid},
		{"null title", work.Ref, Patch{Title: Field[string]{Set: true, Null: true}}, ErrInvalid},
		{"null description", work.Ref, Patch{Description: Field[string]{Set: true, Null: true}}, ErrInvalid},
		{"null refs", work.Ref, Patch{ExternalRefs: Field[[]domain.ExternalRef]{Set: true, Null: true}}, ErrInvalid},
		{"long description", work.Ref, Patch{Description: Field[string]{Set: true, Value: strings.Repeat("a", 20001)}}, ErrInvalid},
		{"cadence on work", work.Ref, Patch{CadenceSeconds: Field[int64]{Set: true, Value: 60}}, ErrInvalid},
		{"next check on work", work.Ref, Patch{NextCheckAt: Field[time.Time]{Set: true, Value: t0}}, ErrInvalid},
		{"end before start", work.Ref, Patch{PlannedStart: Field[time.Time]{Set: true, Value: t0.Add(time.Hour)}, PlannedEnd: Field[time.Time]{Set: true, Value: t0}}, ErrInvalid},
		{"cadence 59", work.Ref, Patch{CadenceSeconds: Field[int64]{Set: true, Value: cad}}, ErrInvalid},
	} {
		events0, tasks0 := count(t, s, `SELECT count(*) FROM events`), count(t, s, `SELECT count(*) FROM tasks WHERE updated_at <> created_at`)
		if _, err := s.Patch(ctx, who, "key:caller", tc.ref, tc.p); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if count(t, s, `SELECT count(*) FROM events`) != events0 || count(t, s, `SELECT count(*) FROM tasks WHERE updated_at <> created_at`) != tasks0 {
			t.Errorf("%s changed a row or an event", tc.name)
		}
	}
	// A closed initiative refuses edits to its open tasks, and a watch task cannot lose its cadence.
	cadence := int64(60)
	w, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "w", Kind: "watch", CadenceSeconds: &cadence})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Patch(ctx, who, "key:caller", w.Ref, Patch{CadenceSeconds: Field[int64]{Set: true, Null: true}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("clearing a watch cadence = %v, want ErrInvalid", err)
	}
	if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE initiatives SET status = 'closed' WHERE id = ?`, ini.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Patch(ctx, who, "key:caller", late.Ref, Patch{Title: title}); !errors.Is(err, ErrInitiativeClosed) {
		t.Errorf("patch in a closed initiative = %v, want ErrInitiativeClosed", err)
	}
}

func TestCountsForCountsTheTasksOfEachInitiativeByStatus(t *testing.T) {
	s := newStore(t)
	a, b, empty := initiative(t, s, "aa"), initiative(t, s, "bb"), initiative(t, s, "cc")
	mk(t, s, "aa", "one")
	two := mk(t, s, "aa", "two")
	three := mk(t, s, "aa", "three")
	mk(t, s, "aa", "four")
	mk(t, s, "bb", "other")
	if _, err := s.Done(ctx, who, "key:caller", two.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(ctx, who, "key:caller", three.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	got, err := s.CountsFor(ctx, []string{a.ID, b.ID, empty.ID})
	want := map[string]Counts{a.ID: {Open: 2, Done: 1, Closed: 1}, b.ID: {Open: 1}, empty.ID: {}}
	if err != nil || len(got) != 3 || got[a.ID] != want[a.ID] || got[b.ID] != want[b.ID] || got[empty.ID] != want[empty.ID] {
		t.Errorf("CountsFor = %+v, %v, want %+v", got, err, want)
	}
	if got, err := s.CountsFor(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("no ids = %+v, %v", got, err)
	}
	if got, err := s.CountsFor(ctx, []string{a.ID}); err != nil || len(got) != 1 {
		t.Errorf("one id = %+v, %v", got, err)
	}
}
