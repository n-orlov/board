package domain

import (
	"reflect"
	"testing"
)

func TestDependencyCycle(t *testing.T) {
	// c is blocked by b, b by a: the edges are task -> its blockers.
	chain := map[string][]string{"c": {"b"}, "b": {"a"}}
	for _, tc := range []struct {
		name     string
		edges    map[string][]string
		task, by string
		want     []string
	}{
		{"no edges", nil, "a", "b", nil},
		{"self", nil, "a", "a", []string{"a", "a"}},
		{"2-cycle", map[string][]string{"b": {"a"}}, "a", "b", []string{"a", "b", "a"}},
		{"3-cycle", chain, "a", "c", []string{"a", "c", "b", "a"}},
		{"4-cycle", map[string][]string{"d": {"c"}, "c": {"b"}, "b": {"a"}}, "a", "d", []string{"a", "d", "c", "b", "a"}},
		{"chain forward is no cycle", chain, "d", "c", nil},
		{"existing edge's reverse direction is no cycle", chain, "c", "a", nil},
		{"diamond is no cycle", map[string][]string{"d": {"b", "c"}, "b": {"a"}, "c": {"a"}}, "e", "d", nil},
		{"shortest path wins", map[string][]string{"d": {"x", "a"}, "x": {"y"}, "y": {"a"}}, "a", "d", []string{"a", "d", "a"}},
		{"ties pick the smaller id", map[string][]string{"d": {"q", "p"}, "p": {"a"}, "q": {"a"}}, "a", "d", []string{"a", "d", "p", "a"}},
		{"a loop elsewhere does not hang", map[string][]string{"x": {"y"}, "y": {"x"}}, "a", "x", nil},
		{"unrelated component", map[string][]string{"x": {"y"}}, "a", "b", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DependencyCycle(tc.edges, tc.task, tc.by); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DependencyCycle = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDependencyCycleDoesNotChangeItsInput(t *testing.T) {
	edges := map[string][]string{"d": {"q", "p"}, "p": {"a"}}
	DependencyCycle(edges, "a", "d")
	if !reflect.DeepEqual(edges["d"], []string{"q", "p"}) {
		t.Errorf("input reordered: %v", edges["d"])
	}
}
