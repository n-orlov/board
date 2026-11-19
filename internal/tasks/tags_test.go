package tasks

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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
