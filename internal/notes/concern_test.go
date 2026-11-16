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

// A note that concerns the actor for several reasons is counted once, whichever
// reasons overlap, with and without the after cursor.
func TestConcerningCountsANoteOnceWhateverReasonsOverlap(t *testing.T) {
	e := newEnv(t)
	first := e.postAs(t, bob, New{Summary: "broadcast mentioning me", Mentions: []string{me.Handle}})
	e.postAs(t, bob, New{Task: "aa-1", Resource: "db:x", Summary: "task, resource and mention", Mentions: []string{me.Handle}})
	e.postAs(t, bob, New{Task: "aa-1", Resource: "db:x", Summary: "task and resource"})
	e.postAs(t, bob, New{Task: "bb-1", Resource: "db:x", Summary: "resource only"})
	e.postAs(t, bob, New{Task: "bb-1", Summary: "not for me", Mentions: []string{"x" + me.Handle}})
	e.postAs(t, bob, New{Summary: "broadcast"})
	c := Concern{Handle: me.Handle, Tasks: []string{"aa-1"}, Resources: []string{"db:x"}}
	got, more, err := e.S.Concerning(ctx, c, 1)
	if err != nil || len(got) != 1 || more != 4 {
		t.Fatalf("first page = %s, %d, %v; want one note and 4 more", noteRefs(got), more, err)
	}
	c.After = first.ID
	got, more, err = e.S.Concerning(ctx, c, 2)
	if err != nil || noteRefs(got) != "N2,N3" || more != 2 {
		t.Errorf("after the first = %s, %d, %v; want N2,N3 and 2 more", noteRefs(got), more, err)
	}
}

// The count of the broadcasts walks the partial broadcast index, not the index
// of a column that is NULL for almost every note (migration 00011).
func TestTheBroadcastCountWalksTheBroadcastIndex(t *testing.T) {
	e := newEnv(t)
	rows, err := e.S.Store.Reader().QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT k.id FROM notes k WHERE `+broadcastSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if !strings.Contains(strings.Join(plan, "; "), "notes_broadcast") {
		t.Errorf("the broadcast count's plan = %v, want it to use notes_broadcast", plan)
	}
}

// A task named by its ULID in TaskIDs concerns the actor like one named by ref in
// Tasks, and the two lists add up.
func TestConcerningTakesTaskIDsAsWellAsRefs(t *testing.T) {
	e := newEnv(t)
	byRef := e.postAs(t, bob, New{Task: "aa-1", Summary: "on a task named by ref"})
	byID := e.postAs(t, bob, New{Task: "aa-2", Summary: "on a task named by id"})
	e.postAs(t, bob, New{Task: "bb-1", Summary: "on a task of no concern"})
	var id string
	if err := e.S.Store.Reader().QueryRowContext(ctx, `SELECT t.id FROM tasks t JOIN notes k ON k.task_id = t.id WHERE k.id = ?`, byID.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	got, more, err := e.S.Concerning(ctx, Concern{Handle: me.Handle, Tasks: []string{"aa-1"}, TaskIDs: []string{id}}, 20)
	if want := byRef.Ref + "," + byID.Ref; err != nil || noteRefs(got) != want || more != 0 {
		t.Errorf("Concerning = %s, %d, %v; want %s", noteRefs(got), more, err, want)
	}
}
