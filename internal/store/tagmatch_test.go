package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestTagMatchWithNoTagsIsEmpty(t *testing.T) {
	cond, args := TagMatch("t.id", "t.initiative_id", nil)
	if cond != "" || args != nil {
		t.Fatalf("got %q %v, want nothing", cond, args)
	}
}

func TestTagMatchChecksOwnAndInitiativeTagsOfAPlainTag(t *testing.T) {
	cond, args := TagMatch("t.id", "t.initiative_id", []string{"web"})
	if want := []any{"web", "web"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for _, part := range []string{"task_tags", "ttg.task_id = t.id", "initiative_tags", "itg.initiative_id = t.initiative_id"} {
		if !strings.Contains(cond, part) {
			t.Errorf("%q lacks %q", cond, part)
		}
	}
	if strings.Contains(cond, "leases") {
		t.Errorf("a plain tag must not look at leases: %q", cond)
	}
}

func TestTagMatchAddsTheLeaseHolderForAnActorKind(t *testing.T) {
	cond, args := TagMatch("t.id", "t.initiative_id", []string{"ralph", "work"})
	want := []any{"ralph", "ralph", 6, "ralph:", "work", "work"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	if got := strings.Count(cond, " AND (EXISTS"); got != 1 {
		t.Errorf("tags are ANDed, one join between two: %q", cond)
	}
	if got := strings.Count(cond, "leases"); got != 1 {
		t.Errorf("only the actor kind reads leases, got %d in %q", got, cond)
	}
}

func TestIsActorKind(t *testing.T) {
	for _, k := range []string{"ralph", "claude", "pi", "codex", "deck"} {
		if !isActorKind(k) {
			t.Errorf("%s is an actor kind", k)
		}
	}
	for _, k := range []string{"", "Ralph", "web", "ralph:"} {
		if isActorKind(k) {
			t.Errorf("%q is not an actor kind", k)
		}
	}
}
