package tasks

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
)

func TestCreateStoresNormalisedTagsAndEveryReadShowsThem(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	got, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "t", Tags: []string{"Work", " home ", "work", "a.b_c:d-e"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.b_c:d-e", "home", "work"}
	if !reflect.DeepEqual(got.Tags, want) {
		t.Fatalf("created tags = %v, want %v", got.Tags, want)
	}
	plain := mk(t, s, "aa", "plain")
	if plain.Tags == nil || len(plain.Tags) != 0 {
		t.Fatalf("a task with no tags has Tags %#v, want an empty list", plain.Tags)
	}
	read, err := s.Get(ctx, got.Ref)
	if err != nil || !reflect.DeepEqual(read.Tags, want) {
		t.Fatalf("Get tags = %v, %v, want %v", read.Tags, err, want)
	}
	page, err := s.List(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !reflect.DeepEqual(page.Items[0].Tags, want) || page.Items[1].Tags == nil || len(page.Items[1].Tags) != 0 {
		t.Fatalf("listed tags = %v and %v, want %v and an empty list", page.Items[0].Tags, page.Items[1].Tags, want)
	}
	if n := count(t, s, `SELECT count(*) FROM task_tags`); n != 3 {
		t.Fatalf("task_tags holds %d rows, want 3", n)
	}
}

func TestCreateRefusesABadTagAndWritesNothing(t *testing.T) {
	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, "t"+strings.Repeat("x", i))
	}
	for name, tags := range map[string][]string{
		"a space": {"has space"}, "a leading dash": {"ok", "-lead"}, "empty": {""}, "too long": {strings.Repeat("a", 41)}, "21 tags": many,
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			initiative(t, s, "aa")
			_, err := s.Create(ctx, who, "key:caller", New{Initiative: "aa", Title: "t", Tags: tags})
			var bad InvalidError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || bad.Next == "" {
				t.Fatalf("Create = %v, want an InvalidError with a next", err)
			}
			if n := count(t, s, `SELECT (SELECT count(*) FROM tasks) + (SELECT count(*) FROM task_tags) + (SELECT count(*) FROM events WHERE type = 'task_created')`); n != 0 {
				t.Fatalf("a refused create left %d rows", n)
			}
			if got := mk(t, s, "aa", "after"); got.Ref != "aa-1" {
				t.Fatalf("the next task is %s, want aa-1: the number was used up", got.Ref)
			}
		})
	}
}

func TestAddTagAndRemoveTagWriteTheRowAndOneTaskUpdatedEventEach(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	task := mk(t, s, "aa", "t")
	got, err := s.AddTag(ctx, who, "key:caller", task.Ref, " Work ")
	if err != nil || !reflect.DeepEqual(got.Tags, []string{"work"}) || !got.UpdatedAt.After(task.UpdatedAt) {
		t.Fatalf("AddTag = %+v, %v, want tags [work] and a later updated_at", got, err)
	}
	if got, err = s.AddTag(ctx, who, "key:caller", task.Ref, "home"); err != nil || !reflect.DeepEqual(got.Tags, []string{"home", "work"}) {
		t.Fatalf("second AddTag = %+v, %v", got.Tags, err)
	}
	if got, err = s.RemoveTag(ctx, who, "key:caller", task.Ref, "WORK"); err != nil || !reflect.DeepEqual(got.Tags, []string{"home"}) {
		t.Fatalf("RemoveTag = %+v, %v", got.Tags, err)
	}
	if n := count(t, s, `SELECT count(*) FROM events WHERE type = 'task_updated' AND data LIKE '%"change":"added"%' AND data LIKE '%"tag":"work"%'`); n != 1 {
		t.Fatalf("%d added work events, want 1", n)
	}
	if n := count(t, s, `SELECT count(*) FROM events WHERE type = 'task_updated' AND data LIKE '%"change":"removed"%' AND data LIKE '%"tag":"work"%' AND data LIKE '%"changed":["tags"]%'`); n != 1 {
		t.Fatalf("%d removed work events, want 1", n)
	}
	if n := count(t, s, `SELECT count(*) FROM events WHERE type = 'task_updated'`); n != 3 {
		t.Fatalf("%d task_updated events, want 3", n)
	}
}

func TestATagChangeThatChangesNothingWritesNothing(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	task := mk(t, s, "aa", "t")
	if _, err := s.AddTag(ctx, who, "key:caller", task.Ref, "work"); err != nil {
		t.Fatal(err)
	}
	actors, events := count(t, s, `SELECT COUNT(*) FROM actors`), count(t, s, `SELECT COUNT(*) FROM events`)
	if got, err := s.AddTag(ctx, audit2("new@host"), "key:other", task.Ref, "Work"); err != nil || len(got.Tags) != 1 {
		t.Fatalf("AddTag of a held tag = %+v, %v", got.Tags, err)
	}
	if got, err := s.RemoveTag(ctx, audit2("new@host"), "key:other", task.Ref, "absent"); err != nil || len(got.Tags) != 1 {
		t.Fatalf("RemoveTag of a missing tag = %+v, %v", got.Tags, err)
	}
	if count(t, s, `SELECT COUNT(*) FROM actors`) != actors || count(t, s, `SELECT COUNT(*) FROM events`) != events || count(t, s, `SELECT COUNT(*) FROM task_tags`) != 1 {
		t.Error("a change that changes nothing wrote something")
	}
}

func TestATagChangeRefusedWritesNothing(t *testing.T) {
	s := newStore(t)
	initiative(t, s, "aa")
	full := mk(t, s, "aa", "full")
	for i := 0; i < 20; i++ {
		if _, err := s.AddTag(ctx, who, "key:caller", full.Ref, "t"+strings.Repeat("x", i)); err != nil {
			t.Fatal(err)
		}
	}
	closed := mk(t, s, "aa", "closed")
	if _, err := s.Close(ctx, who, "key:caller", closed.Ref, "dropped"); err != nil {
		t.Fatal(err)
	}
	before := count(t, s, `SELECT (SELECT count(*) FROM events) + (SELECT count(*) FROM task_tags) + (SELECT count(*) FROM actors)`)
	for name, tc := range map[string]struct {
		ref, tag string
		remove   bool
		want     error
	}{
		"a bad tag":             {full.Ref, "has space", false, ErrInvalid},
		"a bad tag on remove":   {full.Ref, "-lead", true, ErrInvalid},
		"a 21st tag":            {full.Ref, "extra", false, ErrInvalid},
		"an unknown task":       {"aa-99", "work", false, ErrNotFound},
		"an unknown task on rm": {"aa-99", "work", true, ErrNotFound},
		"a closed task":         {closed.Ref, "work", false, ErrClosed},
		"a closed task on rm":   {closed.Ref, "work", true, ErrClosed},
	} {
		var err error
		if tc.remove {
			_, err = s.RemoveTag(ctx, who, "key:caller", tc.ref, tc.tag)
		} else {
			_, err = s.AddTag(ctx, who, "key:caller", tc.ref, tc.tag)
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	if after := count(t, s, `SELECT (SELECT count(*) FROM events) + (SELECT count(*) FROM task_tags) + (SELECT count(*) FROM actors)`); after != before {
		t.Errorf("refused changes moved the row count from %d to %d", before, after)
	}
}

func TestTagMarksCarryOwnInitiativeAndActorKindTagsEachWithItsSource(t *testing.T) {
	s := newStore(t)
	if _, err := (initiatives.Store{Store: s.Store, Now: s.Now}).Create(ctx, who, "key:c", initiatives.New{Slug: "aa", Title: "I", Tags: []string{"home", "ralph"}}); err != nil {
		t.Fatal(err)
	}
	tk, err := s.Create(ctx, who, "key:c", New{Initiative: "aa", Title: "t", Tags: []string{"work", "ralph"}})
	if err != nil {
		t.Fatal(err)
	}
	ls := leases.Store{Store: s.Store, Now: s.Now}
	for _, h := range []string{"codex:a:b", "foo:x", "ralph", "claude:s", "codex:z"} {
		l := startLease(t, ls, audit.Actor{Handle: h}, tk.Ref, "work")
		if h == "codex:a:b" || h == "foo:x" {
			if _, err := ls.End(ctx, audit.Actor{Handle: h}, "key:c", l.Ref, "done"); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := s.Get(ctx, tk.Ref)
	if err != nil {
		t.Fatal(err)
	}
	want := []TagMark{
		{"ralph", "task"}, {"work", "task"}, {"home", "initiative"}, {"ralph", "initiative"},
		{"claude", "actor"}, {"codex", "actor"},
	}
	if !reflect.DeepEqual(got.TagMarks, want) {
		t.Fatalf("TagMarks = %v, want %v", got.TagMarks, want)
	}
	if !reflect.DeepEqual(got.Tags, []string{"ralph", "work"}) || count(t, s, `SELECT count(*) FROM task_tags`) != 2 {
		t.Fatalf("derived tags leaked into the task's own tags: %v", got.Tags)
	}
	plain := mk(t, s, "aa", "plain")
	page, err := s.List(ctx, Query{})
	if err != nil || len(page.Items) != 2 || !reflect.DeepEqual(page.Items[1].TagMarks, []TagMark{{"home", "initiative"}, {"ralph", "initiative"}}) || plain.Ref == "" {
		t.Fatalf("list marks = %+v, %v", page.Items, err)
	}
}

func TestTaggedByRefGivesEachNamedTaskTheTagsAndMarksAReadGives(t *testing.T) {
	s := newStore(t)
	ini := initiatives.Store{Store: s.Store, Now: s.Now}
	for _, in := range []initiatives.New{{Slug: "aa", Title: "A", Tags: []string{"home"}}, {Slug: "b-b", Title: "B"}} {
		if _, err := ini.Create(ctx, who, "key:c", in); err != nil {
			t.Fatal(err)
		}
	}
	one, err := s.Create(ctx, who, "key:c", New{Initiative: "aa", Title: "one", Tags: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	two := mk(t, s, "b-b", "two")
	plain := mk(t, s, "aa", "plain")
	startLease(t, leases.Store{Store: s.Store, Now: s.Now}, audit.Actor{Handle: "pi:x"}, one.Ref, "work")

	got, err := s.TaggedByRef(ctx, []string{one.Ref, two.Ref, "zz-9", plain.Ref})
	if err != nil || len(got) != 3 {
		t.Fatalf("TaggedByRef = %v, %v, want the three tasks that exist", got, err)
	}
	for _, ref := range []string{one.Ref, two.Ref, plain.Ref} {
		read, err := s.Get(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got[ref].Tags, read.Tags) || !reflect.DeepEqual(got[ref].TagMarks, read.TagMarks) {
			t.Errorf("%s: TaggedByRef %+v, a read gives tags %v marks %v", ref, got[ref], read.Tags, read.TagMarks)
		}
	}
	if m := got[two.Ref]; m.Tags == nil || m.TagMarks == nil || len(m.Tags)+len(m.TagMarks) != 0 {
		t.Errorf("a task with no tags has %#v, want empty lists, never nil", m)
	}
	if none, err := s.TaggedByRef(ctx, nil); err != nil || len(none) != 0 {
		t.Errorf("no refs gave %v, %v", none, err)
	}
}

func TestMarksReducesHoldersToTheirKindsInKindOrder(t *testing.T) {
	got := Marks([]string{"b"}, []string{"a", "pi"}, []string{"pi:x", "claude:y", "pi:z", "foo:q", "ralph", ""})
	want := []TagMark{{"b", "task"}, {"a", "initiative"}, {"pi", "initiative"}, {"claude", "actor"}, {"pi", "actor"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Marks = %v, want %v", got, want)
	}
	if empty := Marks(nil, nil, nil); empty == nil || len(empty) != 0 {
		t.Fatalf("Marks of nothing = %#v, want an empty list", empty)
	}
}
