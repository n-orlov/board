package initiatives

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReopenOfAnOpenOrUnknownInitiativeIsRefusedAndWritesNothing(t *testing.T) {
	s := newStore(t)
	mk(t, s, "aa")
	before := count(t, s, "events")
	_, err := s.Reopen(context.Background(), who, "key:r", "aa")
	var c ConflictError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &c) || !strings.Contains(c.Msg, "aa is open") || c.Next == "" {
		t.Fatalf("reopen open = %v", err)
	}
	if count(t, s, "events") != before {
		t.Error("a refusal wrote an event")
	}
	if _, err := s.Reopen(context.Background(), who, "key:r", "zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown = %v", err)
	}
}

func TestReopenClearsTheClosedFieldsAndAppendsOneEvent(t *testing.T) {
	s := newStore(t)
	made := mk(t, s, "aa")
	if _, err := s.Store.Writer().ExecContext(context.Background(),
		`UPDATE initiatives SET status = 'closed', closed_at = 5, closed_by = created_by WHERE id = ?`, made.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Reopen(context.Background(), audit2("op@host"), "key:r", "AA")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "open" || got.ClosedAt != nil || got.ClosedBy != "" || got.ID != made.ID {
		t.Fatalf("reopened = %+v", got)
	}
	var typ, principal, summary string
	if err := s.Store.Reader().QueryRowContext(context.Background(),
		`SELECT type, principal, summary FROM events WHERE type = ?`, EventReopened).Scan(&typ, &principal, &summary); err != nil {
		t.Fatal(err)
	}
	if principal != "key:r" || !strings.Contains(summary, "aa") {
		t.Errorf("event = %s %s %s", typ, principal, summary)
	}
}

func TestMarkClosedRefusesAClosedInitiative(t *testing.T) {
	s := newStore(t)
	made := mk(t, s, "aa")
	if _, err := s.Store.Writer().ExecContext(context.Background(),
		`UPDATE initiatives SET status = 'closed', closed_at = 5, closed_by = created_by WHERE id = ?`, made.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := s.Store.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = s.MarkClosed(context.Background(), tx, made, "ACTOR", 1000)
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "already closed") {
		t.Errorf("err = %v", err)
	}
}
