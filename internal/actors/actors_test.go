package actors

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/store"
)

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) Store {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	return Store{Store: st, Now: func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }}
}

func column(t *testing.T, s Store, handle string) (rows int, seen *int) {
	t.Helper()
	q := s.Store.Reader().QueryRowContext(context.Background(), `SELECT count(*), max(protocol_seen) FROM actors WHERE handle = ?`, handle)
	var v *int
	if err := q.Scan(&rows, &v); err != nil {
		t.Fatal(err)
	}
	return rows, v
}

func TestRecordProtocolSeenCreatesTheActorAndKeepsTheHighestVersion(t *testing.T) {
	s := newStore(t)
	who := audit.Actor{Handle: "claude@host"}
	for _, c := range []struct{ send, want int }{{0, 0}, {3, 3}, {1, 3}, {3, 3}, {4, 4}} {
		got, err := s.RecordProtocolSeen(context.Background(), who, c.send)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Fatalf("after sending %d the column holds %d, want %d", c.send, got, c.want)
		}
	}
	rows, seen := column(t, s, "claude@host")
	if rows != 1 || seen == nil || *seen != 4 {
		t.Fatalf("actor rows %d, protocol_seen %v, want 1 and 4", rows, seen)
	}
}

func TestRecordProtocolSeenKeepsEachActorsOwnVersion(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.RecordProtocolSeen(ctx, audit.Actor{Handle: "a@h"}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordProtocolSeen(ctx, audit.Actor{Handle: "b@h"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, seen := column(t, s, "a@h"); seen == nil || *seen != 2 {
		t.Fatalf("a@h protocol_seen %v, want 2", seen)
	}
	if _, seen := column(t, s, "b@h"); seen == nil || *seen != 0 {
		t.Fatalf("b@h protocol_seen %v, want 0", seen)
	}
}

func TestRecordProtocolSeenRefusesANegativeVersionAndWritesNothing(t *testing.T) {
	s := newStore(t)
	_, err := s.RecordProtocolSeen(context.Background(), audit.Actor{Handle: "a@h"}, -1)
	if !errors.Is(err, ErrNegativeVersion) {
		t.Fatalf("error %v, want ErrNegativeVersion", err)
	}
	if rows, _ := column(t, s, "a@h"); rows != 0 {
		t.Fatalf("%d actor rows, want 0", rows)
	}
}

func TestNowDefaultsToTheClock(t *testing.T) {
	s := Store{}
	if d := time.Since(s.now()); d < 0 || d > time.Minute {
		t.Fatalf("default clock is %v away from now", d)
	}
}

func exec(t *testing.T, s Store, stmt string) {
	t.Helper()
	if _, err := s.Store.Writer().ExecContext(context.Background(), stmt); err != nil {
		t.Fatal(err)
	}
}

func TestRecordProtocolSeenReportsADatabaseThatFailsAtEachStep(t *testing.T) {
	who := audit.Actor{Handle: "a@h"}
	for name, c := range map[string]struct {
		break_ func(*testing.T, Store)
		want   string
	}{
		"the database is closed":   {func(t *testing.T, s Store) { _ = s.Store.Close() }, "begin"},
		"the actors table is gone": {func(t *testing.T, s Store) { exec(t, s, `DROP TABLE actors`) }, "record actor"},
		"the update is refused": {func(t *testing.T, s Store) {
			exec(t, s, `CREATE TRIGGER no_update BEFORE UPDATE ON actors BEGIN SELECT RAISE(ABORT, 'refused'); END`)
		}, "record protocol seen"},
		"the column cannot be read back": {func(t *testing.T, s Store) {
			exec(t, s, `CREATE TRIGGER wipe AFTER UPDATE OF protocol_seen ON actors BEGIN UPDATE actors SET protocol_seen = NULL WHERE id = NEW.id; END`)
		}, "record protocol seen"},
	} {
		s := newStore(t)
		c.break_(t, s)
		_, err := s.RecordProtocolSeen(context.Background(), who, 1)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one containing %q", name, err, c.want)
		}
	}
}
