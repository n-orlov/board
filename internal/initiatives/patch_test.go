package initiatives

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func str(v string) Field[string] { return Field[string]{Set: true, Value: v} }

func updatedEvents(t *testing.T, s Store) []string {
	t.Helper()
	rows, err := s.Store.Reader().QueryContext(context.Background(), `SELECT data FROM events WHERE type = ? ORDER BY n`, EventUpdated)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestPatchChangesEachFieldAndNamesThemInOrder(t *testing.T) {
	s := newStore(t)
	made := mk(t, s, "aa", "x")
	got, err := s.Patch(context.Background(), who, "key:p", "AA", Patch{
		Title: str("  New  "), Description: str("d"), Tags: Field[[]string]{Set: true, Value: []string{"Y", "z", "y"}},
		TargetStart: str("2026-01-01"), TargetEnd: str("2026-02-01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != made.ID || got.Title != "New" || got.Description != "d" || !reflect.DeepEqual(got.Tags, []string{"y", "z"}) ||
		got.TargetStart != "2026-01-01" || got.TargetEnd != "2026-02-01" {
		t.Fatalf("patched = %+v", got)
	}
	want := `{"changed":["title","description","tags","target_start","target_end"],"slug":"aa"}`
	if ev := updatedEvents(t, s); len(ev) != 1 || ev[0] != want {
		t.Fatalf("events = %v, want %s", ev, want)
	}
}

func TestPatchClearsDatesAndEmptiesTags(t *testing.T) {
	s := newStore(t)
	mk(t, s, "aa", "x")
	if _, err := s.Patch(context.Background(), who, "k", "aa", Patch{TargetStart: str("2026-01-01")}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Patch(context.Background(), who, "k", "aa", Patch{
		TargetStart: Field[string]{Set: true, Null: true}, Tags: Field[[]string]{Set: true, Value: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetStart != "" || len(got.Tags) != 0 {
		t.Fatalf("cleared = %+v", got)
	}
	if ev := updatedEvents(t, s); len(ev) != 2 || ev[1] != `{"changed":["tags","target_start"],"slug":"aa"}` {
		t.Fatalf("events = %v", ev)
	}
}

func TestPatchThatChangesNothingWritesNothing(t *testing.T) {
	s := newStore(t)
	mk(t, s, "aa", "x")
	events, actors := count(t, s, "events"), count(t, s, "actors")
	quiet := who
	quiet.Handle = "quiet@host"
	for name, p := range map[string]Patch{
		"empty": {}, "same values": {Title: str("T aa"), Tags: Field[[]string]{Set: true, Value: []string{"X"}}, Description: str("")},
		"clear an unset date": {TargetEnd: Field[string]{Set: true, Null: true}},
	} {
		if _, err := s.Patch(context.Background(), quiet, "k", "aa", p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if count(t, s, "events") != events || count(t, s, "actors") != actors || len(updatedEvents(t, s)) != 0 {
		t.Error("a no-op patch wrote an event or an actor")
	}
}

func TestPatchRefusalsChangeNothing(t *testing.T) {
	s := newStore(t)
	mk(t, s, "aa", "x")
	events, actors := count(t, s, "events"), count(t, s, "actors")
	null := func() Field[string] { return Field[string]{Set: true, Null: true} }
	for name, p := range map[string]Patch{
		"empty title":    {Title: str("  ")},
		"long title":     {Title: str(strings.Repeat("t", 201))},
		"null title":     {Title: null()},
		"null desc":      {Description: null()},
		"null tags":      {Tags: Field[[]string]{Set: true, Null: true}},
		"bad tag":        {Tags: Field[[]string]{Set: true, Value: []string{"no way"}}},
		"long desc":      {Description: str(strings.Repeat("d", 20001))},
		"bad date":       {TargetStart: str("tomorrow")},
		"end before":     {TargetStart: str("2026-02-01"), TargetEnd: str("2026-01-01")},
		"new start late": {TargetEnd: str("2026-01-01"), TargetStart: str("2026-03-01")},
	} {
		_, err := s.Patch(context.Background(), who, "k", "aa", p)
		var bad InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || bad.Next == "" {
			t.Errorf("%s: %v, want an InvalidError with a next", name, err)
		}
	}
	if count(t, s, "events") != events || count(t, s, "actors") != actors {
		t.Error("a refused patch wrote an event or an actor")
	}
	got, _ := s.Get(context.Background(), "aa")
	if got.Title != "T aa" || !reflect.DeepEqual(got.Tags, []string{"x"}) {
		t.Errorf("a refused patch changed %+v", got)
	}
}

func TestPatchChecksTheEndAgainstTheStoredStart(t *testing.T) {
	s := newStore(t)
	mk(t, s, "aa")
	if _, err := s.Patch(context.Background(), who, "k", "aa", Patch{TargetStart: str("2026-03-01")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Patch(context.Background(), who, "k", "aa", Patch{TargetEnd: str("2026-02-01")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("end before the stored start: %v", err)
	}
}

func TestPatchUnknownIsNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.Patch(context.Background(), who, "k", "nope", Patch{Title: str("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if count(t, s, "actors") != 0 {
		t.Error("a refused patch recorded an actor")
	}
}
