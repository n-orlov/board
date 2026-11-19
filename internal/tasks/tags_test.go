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
