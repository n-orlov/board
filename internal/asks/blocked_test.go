package asks

import (
	"testing"

	"github.com/n-orlov/village-hall/internal/tasks"
)

func blockedOf(t *testing.T, e env, ref string) bool {
	t.Helper()
	got, err := e.Tasks.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	return got.Blocked
}

func TestTaskBlockedFollowsItsOpenBlockingAsks(t *testing.T) {
	e := newEnv(t)
	if blockedOf(t, e, "aa-1") {
		t.Fatal("a task with no ask is blocked")
	}
	e.raise(t, New{Task: "aa-1", Question: "soft?"})
	if blockedOf(t, e, "aa-1") {
		t.Error("a non-blocking open ask blocks")
	}
	hard := e.raise(t, New{Task: "aa-1", Question: "hard?", Blocking: true})
	other := e.raise(t, New{Task: "bb-1", Question: "other?", Blocking: true})
	if !blockedOf(t, e, "aa-1") || !blockedOf(t, e, "bb-1") {
		t.Error("an open blocking ask does not block its task")
	}
	if _, err := e.S.Answer(ctx, op, "oidc:n", hard.Ref, Reply{Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	if blockedOf(t, e, "aa-1") {
		t.Error("an answered ask still blocks")
	}
	if _, err := e.S.Withdraw(ctx, me, "key:x", other.Ref, ""); err != nil {
		t.Fatal(err)
	}
	if blockedOf(t, e, "bb-1") {
		t.Error("a withdrawn ask still blocks")
	}
}

func TestBlockedFilterFollowsTheSameRule(t *testing.T) {
	e := newEnv(t)
	e.raise(t, New{Task: "aa-1", Question: "hard?", Blocking: true})
	e.raise(t, New{Initiative: "aa", Question: "initiative-wide", Blocking: true})
	yes, no := true, false
	page, err := e.Tasks.List(ctx, tasksQuery(&yes))
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref != "aa-1" {
		t.Fatalf("blocked=true: %+v, %v", page.Items, err)
	}
	page, err = e.Tasks.List(ctx, tasksQuery(&no))
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref != "bb-1" {
		t.Fatalf("blocked=false: %+v, %v", page.Items, err)
	}
}

func tasksQuery(blocked *bool) tasks.Query { return tasks.Query{Blocked: blocked} }
