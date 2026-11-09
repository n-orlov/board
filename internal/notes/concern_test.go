package notes

import (
	"errors"
	"strings"
	"testing"
)

func noteRefs(items []Note) string {
	out := make([]string, len(items))
	for i, k := range items {
		out[i] = k.Ref
	}
	return strings.Join(out, ",")
}

func TestConcerningKeepsTasksResourcesMentionsAndBroadcasts(t *testing.T) {
	e := newEnv(t)
	onTask := e.postAs(t, bob, New{Task: "aa-1", Summary: "on my task"})
	e.postAs(t, bob, New{Task: "aa-2", Summary: "on another task"})
	e.postAs(t, bob, New{Initiative: "aa", Summary: "on an initiative"})
	onRes := e.postAs(t, bob, New{Task: "bb-1", Resource: "db:x", Summary: "on my resource"})
	e.postAs(t, bob, New{Resource: "db:y", Task: "bb-1", Summary: "on another resource"})
	mention := e.postAs(t, bob, New{Task: "bb-1", Summary: "hi", Mentions: []string{me.Handle}})
	e.postAs(t, bob, New{Task: "bb-1", Summary: "hi someone else", Mentions: []string{"x" + me.Handle}})
	broadcast := e.postAs(t, bob, New{Summary: "everyone"})
	broadcastRes := e.postAs(t, bob, New{Summary: "everyone about a host", Resource: "host:z"})
	c := Concern{Handle: me.Handle, Tasks: []string{"aa-1"}, Resources: []string{"db:x"}}
	got, more, err := e.S.Concerning(ctx, c, 20)
	want := strings.Join([]string{onTask.Ref, onRes.Ref, mention.Ref, broadcast.Ref, broadcastRes.Ref}, ",")
	if err != nil || noteRefs(got) != want || more != 0 {
		t.Fatalf("Concerning = %s, %d, %v; want %s", noteRefs(got), more, err, want)
	}
	if got[0].HasBody || got[0].Body != "" {
		t.Errorf("a summary carries no body: %+v", got[0])
	}
	got, more, err = e.S.Concerning(ctx, Concern{Handle: me.Handle}, 20)
	if want = strings.Join([]string{mention.Ref, broadcast.Ref, broadcastRes.Ref}, ","); err != nil || noteRefs(got) != want || more != 0 {
		t.Errorf("with no lease Concerning = %s, %v; want %s", noteRefs(got), err, want)
	}
}

func TestConcerningPagesAfterTheCursorAndCountsTheRest(t *testing.T) {
	e := newEnv(t)
	first := e.post(t, New{Summary: "one"})
	e.post(t, New{Summary: "two"})
	e.post(t, New{Summary: "three"})
	got, more, err := e.S.Concerning(ctx, Concern{Handle: me.Handle}, 2)
	if err != nil || noteRefs(got) != "N1,N2" || more != 1 {
		t.Fatalf("first page = %s, %d, %v; want N1,N2 and 1 more", noteRefs(got), more, err)
	}
	got, more, err = e.S.Concerning(ctx, Concern{Handle: me.Handle, After: first.ID}, 20)
	if err != nil || noteRefs(got) != "N2,N3" || more != 0 {
		t.Errorf("after N1 = %s, %d, %v; want N2,N3", noteRefs(got), more, err)
	}
	got, more, err = e.S.Concerning(ctx, Concern{Handle: me.Handle, After: "N3"}, 20)
	if err != nil || len(got) != 0 || got == nil || more != 0 {
		t.Errorf("after the last = %v, %d, %v; want none", got, more, err)
	}
}

func TestConcerningRefusesAnUnknownTaskAndABadAfter(t *testing.T) {
	e := newEnv(t)
	if _, _, err := e.S.Concerning(ctx, Concern{Handle: "a", Tasks: []string{"zz-9"}}, 20); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown task: %v, want ErrNotFound", err)
	}
	if _, _, err := e.S.Concerning(ctx, Concern{Handle: "a", After: "nonsense"}, 20); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad after: %v, want ErrInvalid", err)
	}
}
