package initiatives

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func inboxEvents(t *testing.T, s Store) (n int, handle, kind, principal string) {
	t.Helper()
	err := s.Store.Reader().QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE type = 'initiative_created'`).Scan(&n)
	if err == nil && n > 0 {
		err = s.Store.Reader().QueryRowContext(context.Background(), `SELECT a.handle, a.kind, e.principal FROM events e JOIN actors a ON a.id = e.actor_id WHERE e.type = 'initiative_created'`).Scan(&handle, &kind, &principal)
	}
	if err != nil {
		t.Fatal(err)
	}
	return n, handle, kind, principal
}

func TestEnsureInboxCreatesTheInboxOnceWithOneOperatorEvent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for call, want := range []bool{true, false} {
		made, err := s.EnsureInbox(ctx, "boss")
		if err != nil || made != want {
			t.Fatalf("call %d: made %v, err %v; want %v", call+1, made, err, want)
		}
	}
	got, err := s.Get(ctx, "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Inbox" || got.Description != "Work that fits no other initiative yet." || got.Status != StatusOpen || len(got.Tags) != 0 || got.CreatedBy != "boss" {
		t.Fatalf("inbox = %+v", got)
	}
	n, handle, kind, principal := inboxEvents(t, s)
	if n != 1 || handle != "boss" || kind != "operator" || principal != "system:bootstrap" {
		t.Fatalf("events = %d by %s (%s) under %s, want 1 by boss (operator) under system:bootstrap", n, handle, kind, principal)
	}
}

func TestEnsureInboxLeavesAnExistingInboxAlone(t *testing.T) {
	ctx := context.Background()
	t.Run("another title and tags", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Create(ctx, who, "key:caller", New{Slug: "inbox", Title: "My pile", Tags: []string{"mine"}}); err != nil {
			t.Fatal(err)
		}
		if made, err := s.EnsureInbox(ctx, "boss"); err != nil || made {
			t.Fatalf("made %v, err %v; want neither", made, err)
		}
		got, err := s.Get(ctx, "inbox")
		if err != nil || got.Title != "My pile" || len(got.Tags) != 1 || got.Tags[0] != "mine" {
			t.Fatalf("inbox = %+v, err %v; want the title and tags it had", got, err)
		}
		if n, _, _, _ := inboxEvents(t, s); n != 1 {
			t.Fatalf("%d creation events, want the 1 of the first creation", n)
		}
	})
	t.Run("closed", func(t *testing.T) {
		s := newStore(t)
		made := mk(t, s, "inbox")
		if _, err := s.Store.Writer().ExecContext(ctx, `UPDATE initiatives SET status = 'closed', closed_at = created_at, closed_by = created_by WHERE id = ?`, made.ID); err != nil {
			t.Fatal(err)
		}
		if ok, err := s.EnsureInbox(ctx, "boss"); err != nil || ok {
			t.Fatalf("made %v, err %v; want neither", ok, err)
		}
		if got, err := s.Get(ctx, "inbox"); err != nil || got.Status != StatusClosed {
			t.Fatalf("inbox = %+v, err %v; want it still closed", got, err)
		}
	})
}

func TestEnsureInboxReportsADatabaseThatCannotBeRead(t *testing.T) {
	s := newStore(t)
	_ = s.Store.Close()
	if made, err := s.EnsureInbox(context.Background(), "boss"); err == nil || made {
		t.Fatalf("made %v, err %v; want an error", made, err)
	}
}

// TestEnsureInboxCalledTogetherCreatesOneInboxAndOneEvent runs the start-up of
// several servers at once: whoever loses the race for the slug is not an error.
func TestEnsureInboxCalledTogetherCreatesOneInboxAndOneEvent(t *testing.T) {
	s := newStore(t)
	s.Now = nil // the fixture clock is not safe for concurrent use
	const servers = 8
	var wg sync.WaitGroup
	errs := make(chan error, servers)
	var made atomic.Int32
	for range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.EnsureInbox(context.Background(), "boss")
			if ok {
				made.Add(1)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, _, _, _ := inboxEvents(t, s); made.Load() != 1 || n != 1 || count(t, s, "initiatives") != 1 {
		t.Fatalf("%d calls made it, %d events, %d initiatives; want 1 each", made.Load(), n, count(t, s, "initiatives"))
	}
}
