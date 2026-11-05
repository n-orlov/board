package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open("sqlite://" + filepath.Join(t.TempDir(), "sub", "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReadyOnlyAfterMigrate(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.Ready(ctx); !errors.Is(err, ErrNotMigrated) {
		t.Fatalf("Ready on a fresh database = %v, want ErrNotMigrated", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Ready(ctx); err != nil {
		t.Fatalf("Ready after Migrate = %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate = %v, want idempotent", err)
	}
}

func TestReadyFailsWhenClosed(t *testing.T) {
	s := open(t)
	s.Close()
	if err := s.Ready(context.Background()); err == nil {
		t.Fatal("Ready on a closed database = nil")
	}
}

func TestOpenRejectsOtherSchemes(t *testing.T) {
	if _, err := Open("postgres://x/y"); err == nil {
		t.Fatal("expected an unsupported scheme error")
	}
	if _, err := Open("sqlite://"); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}
