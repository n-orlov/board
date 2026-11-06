package domain

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func i64(v int64) *int64 { return &v }

func TestClampLeaseTTL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *int64
		want int64
	}{
		{"below the floor", i64(59), 60},
		{"above the ceiling", i64(43201), 43200},
		{"absent is the default", nil, 7200},
		{"the floor is kept", i64(60), 60},
		{"the ceiling is kept", i64(43200), 43200},
		{"a value between is kept", i64(3600), 3600},
		{"zero is the floor", i64(0), 60},
		{"negative is the floor", i64(-5), 60},
	} {
		if got := ClampLeaseTTL(tc.in); got != tc.want {
			t.Errorf("%s: ClampLeaseTTL = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestValidateIntentWantsOneToTwoHundredCharacters(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"", false}, {"   ", false}, {"x", true}, {strings.Repeat("x", 200), true}, {strings.Repeat("x", 201), false},
		{strings.Repeat("é", 200), true}, {strings.Repeat("é", 201), false},
	} {
		if err := ValidateIntent(tc.in); (err == nil) != tc.ok {
			t.Errorf("ValidateIntent(%d chars) = %v, want ok=%v", len(tc.in), err, tc.ok)
		}
	}
}

func TestNormalizeResourcesRemovesDuplicatesAndKeepsOrder(t *testing.T) {
	got, err := NormalizeResources([]string{"b", "a", "b", "a", "c"})
	if err != nil || !reflect.DeepEqual(got, []string{"b", "a", "c"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	got, err = NormalizeResources(nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil in = %#v, %v; want an empty non-nil slice", got, err)
	}
}

func TestNormalizeResourcesRefusesTheEmptyTheTooLongAndTooMany(t *testing.T) {
	distinct := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = strings.Repeat("r", i+1)
		}
		return out
	}
	for _, tc := range []struct {
		name string
		in   []string
		ok   bool
	}{
		{"zero characters", []string{"a", ""}, false},
		{"one character", []string{"a"}, true},
		{"100 characters", []string{strings.Repeat("x", 100)}, true},
		{"101 characters", []string{strings.Repeat("x", 101)}, false},
		{"100 multibyte characters", []string{strings.Repeat("é", 100)}, true},
		{"20 resources", distinct(20), true},
		{"21 resources", distinct(21), false},
		{"21 with duplicates counts distinct ones", append(distinct(20), "r", "rr"), true},
	} {
		_, err := NormalizeResources(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestLeaseRefs(t *testing.T) {
	if got := FormatLeaseRef(12); got != "L12" {
		t.Errorf("FormatLeaseRef = %q", got)
	}
	for in, want := range map[string]int64{"L1": 1, "l42": 42} {
		if n, ok := ParseLeaseRef(in); !ok || n != want {
			t.Errorf("ParseLeaseRef(%q) = %d, %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "L", "L0", "L-1", "L+1", "X1", "1", "Lx", "L1a"} {
		if _, ok := ParseLeaseRef(in); ok {
			t.Errorf("ParseLeaseRef(%q) was accepted", in)
		}
	}
}

func hold(lease, holder, task string, res ...string) Holding {
	return Holding{Lease: lease, Holder: holder, Task: task, Intent: "i-" + lease, ExpiresAt: time.Unix(100, 0), Resources: res}
}

func TestConflictsListsOtherLiveHoldersOfSharedResourcesOnly(t *testing.T) {
	mine := []string{"repo:a", "host:x", "file:z"}
	for _, tc := range []struct {
		name string
		live []Holding
		want []Holding
	}{
		{"no other holder", []Holding{hold("L1", "me", "t-1", "repo:a")}, []Holding{}},
		{"nobody at all", nil, []Holding{}},
		{"a disjoint holder", []Holding{hold("L2", "bob", "t-2", "repo:b")}, []Holding{}},
		{"one other holder", []Holding{hold("L1", "me", "t-1", "repo:a"), hold("L2", "bob", "t-2", "repo:a")},
			[]Holding{hold("L2", "bob", "t-2", "repo:a")}},
		{"two other holders", []Holding{hold("L2", "bob", "t-2", "repo:a"), hold("L3", "eve", "t-3", "host:x")},
			[]Holding{hold("L2", "bob", "t-2", "repo:a"), hold("L3", "eve", "t-3", "host:x")}},
		{"the same actor on another task", []Holding{hold("L4", "me", "t-9", "host:x")},
			[]Holding{hold("L4", "me", "t-9", "host:x")}},
		{"only the shared resources are listed, in my order", []Holding{hold("L2", "bob", "t-2", "file:z", "other", "repo:a")},
			[]Holding{hold("L2", "bob", "t-2", "repo:a", "file:z")}},
	} {
		got := Conflicts("L1", mine, tc.live)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestConflictsNeverListsTheLeaseItselfEvenWithAllItsResources(t *testing.T) {
	got := Conflicts("L1", []string{"a", "b"}, []Holding{hold("L1", "me", "t-1", "a", "b")})
	if len(got) != 0 {
		t.Fatalf("a lease conflicted with itself: %+v", got)
	}
}

func TestConflictsWithNoResourcesFindsNothing(t *testing.T) {
	if got := Conflicts("L1", nil, []Holding{hold("L2", "bob", "t-2", "a")}); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestConflictsDoesNotMutateItsInput(t *testing.T) {
	live := []Holding{hold("L2", "bob", "t-2", "x", "a")}
	_ = Conflicts("L1", []string{"a"}, live)
	if !reflect.DeepEqual(live[0].Resources, []string{"x", "a"}) {
		t.Fatalf("input mutated: %v", live[0].Resources)
	}
}
