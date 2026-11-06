package leases

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func refsOf(p Page) []string {
	out := []string{}
	for _, l := range p.Items {
		out = append(out, l.Ref)
	}
	return out
}

// listEnv has L1 (me, aa-1, db:x) live, L2 (bob, aa-2, db:x repo:y) live, L3 (me, aa-2, repo:y) ended
// and L4 (bob, aa-1, db:z) expired.
func listEnv(t *testing.T) env {
	t.Helper()
	e := newEnv(t)
	e.start(t, me, "aa-1", nil, "db:x")
	e.start(t, bob, "aa-2", nil, "db:x", "repo:y")
	e.start(t, me, "aa-2", nil, "repo:y")
	if _, err := e.S.End(ctx, me, "key:x", "L3", ""); err != nil {
		t.Fatal(err)
	}
	e.start(t, bob, "aa-1", secs(60), "db:z")
	e.Clock.add(2 * time.Minute)
	for _, ref := range []string{"L1", "L2"} {
		if _, err := e.S.Renew(ctx, me, "key:x", ref, Renew{}); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestListFiltersByStateActorTaskAndResource(t *testing.T) {
	e := listEnv(t)
	for _, tc := range []struct {
		q    Query
		want []string
	}{
		{Query{}, []string{"L1", "L2"}},
		{Query{State: StateLive}, []string{"L1", "L2"}},
		{Query{State: StateEnded}, []string{"L3"}},
		{Query{State: StateExpired}, []string{"L4"}},
		{Query{State: StateAll}, []string{"L1", "L2", "L3", "L4"}},
		{Query{State: StateAll, Actor: "claude@host"}, []string{"L1", "L3"}},
		{Query{State: StateAll, Task: "aa-1"}, []string{"L1", "L4"}},
		{Query{State: StateAll, Resource: "repo:y"}, []string{"L2", "L3"}},
		{Query{State: StateAll, Resource: "db:x", Actor: "codex@host", Task: "aa-2"}, []string{"L2"}},
		{Query{Resource: "db:none"}, []string{}},
	} {
		p, err := e.S.List(ctx, tc.q)
		if err != nil || !reflect.DeepEqual(refsOf(p), tc.want) {
			t.Errorf("List(%+v) = %v, %v; want %v", tc.q, refsOf(p), err, tc.want)
		}
	}
}

func TestListShowsResourcesSortedAndExpiresDueLeasesFirst(t *testing.T) {
	e := listEnv(t)
	e.Clock.add(3 * time.Hour)
	p, err := e.S.List(ctx, Query{State: StateExpired})
	if err != nil || !reflect.DeepEqual(refsOf(p), []string{"L1", "L2", "L4"}) {
		t.Fatalf("expired = %v, %v", refsOf(p), err)
	}
	if got := p.Items[1].Resources; !reflect.DeepEqual(got, []string{"db:x", "repo:y"}) {
		t.Errorf("resources = %v", got)
	}
	if n := len(e.events(t, EventExpired)); n != 3 {
		t.Errorf("%d lease_expired events, want 3", n)
	}
}

func TestListPagesWithLimitAndAfter(t *testing.T) {
	e := listEnv(t)
	first, err := e.S.List(ctx, Query{State: StateAll, Limit: 3})
	if err != nil || !reflect.DeepEqual(refsOf(first), []string{"L1", "L2", "L3"}) || first.NextAfter != first.Items[2].ID {
		t.Fatalf("first = %v %q, %v", refsOf(first), first.NextAfter, err)
	}
	rest, err := e.S.List(ctx, Query{State: StateAll, Limit: 3, After: first.NextAfter})
	if err != nil || !reflect.DeepEqual(refsOf(rest), []string{"L4"}) || rest.NextAfter != "" {
		t.Errorf("rest = %v %q, %v", refsOf(rest), rest.NextAfter, err)
	}
	exact, _ := e.S.List(ctx, Query{State: StateAll, Limit: 4})
	if exact.NextAfter != "" || len(exact.Items) != 4 {
		t.Errorf("exact fit = %v %q", refsOf(exact), exact.NextAfter)
	}
	byRef, _ := e.S.List(ctx, Query{State: StateAll, After: "l2"})
	if !reflect.DeepEqual(refsOf(byRef), []string{"L3", "L4"}) {
		t.Errorf("after l2 = %v", refsOf(byRef))
	}
}

func TestListRefusesWhatItCannotRead(t *testing.T) {
	e := listEnv(t)
	for name, q := range map[string]Query{
		"state":     {State: "gone"},
		"limit 0":   {Limit: -1},
		"limit big": {Limit: 1001},
		"after":     {After: "nonsense"},
	} {
		var bad InvalidError
		if _, err := e.S.List(ctx, q); !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || bad.Next == "" {
			t.Errorf("%s: err = %v, want an InvalidError with a next", name, err)
		}
	}
	if _, err := e.S.List(ctx, Query{Task: "aa-9"}); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "task aa-9") {
		t.Errorf("unknown task: %v, want ErrNotFound naming it", err)
	}
}

func TestCurrentExpiresDueLeasesBeforeReading(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", secs(60))
	e.Clock.add(2 * time.Minute)
	l, err := e.S.Current(ctx, "l1")
	if err != nil || l.State != "expired" {
		t.Fatalf("Current = %+v, %v; want expired", l, err)
	}
	if _, err := e.S.Current(ctx, "L9"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "lease L9") {
		t.Errorf("Current(L9) = %v", err)
	}
}

func TestStartWithoutATaskIsInvalid(t *testing.T) {
	e := newEnv(t)
	if _, err := e.S.Start(ctx, me, "key:x", Start{Task: " ", Intent: "x"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}
