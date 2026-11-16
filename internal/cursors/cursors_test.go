package cursors

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/notes"
	"github.com/n-orlov/village-hall/internal/store"
)

var (
	ctx = context.Background()
	me  = audit.Actor{Handle: "claude@host"}
	bob = audit.Actor{Handle: "codex@host"}
	t0  = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
)

// newEnv is a migrated SQLite database holding two broadcast notes (so two
// events too), and the cursor store on it.
func newEnv(t *testing.T) (Store, []notes.Note) {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := t0
	ns := notes.Store{Store: st, Now: func() time.Time { return now }}
	var posted []notes.Note
	for _, summary := range []string{"first", "second"} {
		now = now.Add(time.Second)
		k, err := ns.Post(ctx, me, "key:x", notes.New{Summary: summary})
		if err != nil {
			t.Fatal(err)
		}
		posted = append(posted, k)
	}
	return Store{Store: st, Now: func() time.Time { return now }}, posted
}

func TestSetNotesByULIDAndRef(t *testing.T) {
	s, ns := newEnv(t)
	for _, position := range []string{ns[0].ID, strings.ToLower(ns[0].ID), "N1", "n1"} {
		c, err := s.Set(ctx, me, StreamNotes, position)
		if err != nil {
			t.Fatalf("set %q: %v", position, err)
		}
		if c != (Cursor{Stream: "notes", Position: ns[0].ID, Ref: "N1"}) {
			t.Fatalf("set %q = %+v", position, c)
		}
	}
	got, err := s.Get(ctx, me.Handle, StreamNotes)
	if err != nil || got != ns[0].ID {
		t.Fatalf("get = %q, %v", got, err)
	}
	if _, err := s.Set(ctx, me, StreamNotes, "N2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, me.Handle, StreamNotes); got != ns[1].ID {
		t.Fatalf("a second set must replace the position, got %q", got)
	}
}

func TestSetEventsByRefAndID(t *testing.T) {
	s, _ := newEnv(t)
	c, err := s.Set(ctx, me, StreamEvents, "E2")
	if err != nil || c.Ref != "E2" || len(c.Position) != 26 {
		t.Fatalf("set E2 = %+v, %v", c, err)
	}
	again, err := s.Set(ctx, me, StreamEvents, strings.ToLower(c.Position))
	if err != nil || again != c {
		t.Fatalf("set by id = %+v, %v; want %+v", again, err, c)
	}
	if got, _ := s.Get(ctx, me.Handle, StreamEvents); got != c.Position {
		t.Fatalf("get = %q", got)
	}
	if got, _ := s.Get(ctx, me.Handle, StreamNotes); got != "" {
		t.Fatalf("the notes cursor must stay unset, got %q", got)
	}
}

func TestSetRefusals(t *testing.T) {
	s, ns := newEnv(t)
	if _, err := s.Set(ctx, me, "tasks", "N1"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "stream tasks") {
		t.Fatalf("unknown stream: %v", err)
	}
	cases := []struct{ stream, position, want string }{
		{StreamNotes, "N99", "names no note"},
		{StreamNotes, "E1", "names no note"},
		{StreamNotes, "", "names no note"},
		{StreamNotes, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "names no note"},
		{StreamEvents, "E999", "names no event"},
		{StreamEvents, ns[0].ID, "names no event"},
		{StreamEvents, "N1", "names no event"},
	}
	for _, c := range cases {
		_, err := s.Set(ctx, me, c.stream, c.position)
		var bad InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || !strings.Contains(bad.Msg, c.want) || bad.Next == "" {
			t.Errorf("set %s %q: %v", c.stream, c.position, err)
		}
	}
	if got, _ := s.Get(ctx, me.Handle, StreamNotes); got != "" {
		t.Fatalf("a refused set must leave no cursor, got %q", got)
	}
}

func TestCursorsAreOnePerActorAndStream(t *testing.T) {
	s, ns := newEnv(t)
	if _, err := s.Set(ctx, me, StreamNotes, "N1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Ack(ctx, bob, StreamNotes, ns[1].ID); err != nil {
		t.Fatal(err)
	}
	for handle, want := range map[string]string{me.Handle: ns[0].ID, bob.Handle: ns[1].ID, "nobody@host": ""} {
		if got, err := s.Get(ctx, handle, StreamNotes); err != nil || got != want {
			t.Errorf("get %s = %q, %v; want %q", handle, got, err, want)
		}
	}
}

func TestUnknownStreamIsNotFoundOnAckAndGet(t *testing.T) {
	s, _ := newEnv(t)
	if err := s.Ack(ctx, me, "x", "N1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ack: %v", err)
	}
	if _, err := s.Get(ctx, me.Handle, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
}

func TestADatabaseFailureIsNotAnInvalidPositionOrAnUnknownStream(t *testing.T) {
	s, _ := newEnv(t)
	if err := s.Store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ctx, me, StreamNotes, "N1"); err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
		t.Errorf("set on a closed database = %v, want a plain failure", err)
	}
	if err := s.Ack(ctx, me, StreamNotes, "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("ack on a closed database = %v, want a plain failure", err)
	}
	if _, err := s.Get(ctx, me.Handle, StreamNotes); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("get on a closed database = %v, want a plain failure", err)
	}
}
