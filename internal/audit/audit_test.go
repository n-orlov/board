package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/store"
)

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func inTx(t *testing.T, st *store.Store, f func(*sql.Tx)) {
	t.Helper()
	tx, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f(tx)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertActorCreatesOnceBumpsLastSeenAndKeepsItsKind(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	var first, second string
	inTx(t, st, func(tx *sql.Tx) {
		var err error
		if first, err = UpsertActor(context.Background(), tx, d, Actor{Handle: " operator ", Operator: true}, t0); err != nil {
			t.Fatal(err)
		}
		if second, err = UpsertActor(context.Background(), tx, d, Actor{Handle: "operator"}, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	})
	if first != second {
		t.Errorf("ids differ: %s %s", first, second)
	}
	var handle, kind string
	var firstSeen, lastSeen int64
	err := st.Reader().QueryRowContext(context.Background(), `SELECT handle, kind, first_seen_at, last_seen_at FROM actors`).Scan(&handle, &kind, &firstSeen, &lastSeen)
	if err != nil || handle != "operator" || kind != "operator" || firstSeen != store.EncodeTime(t0) || lastSeen != store.EncodeTime(t0.Add(time.Hour)) {
		t.Errorf("actor %q %q %d %d (%v)", handle, kind, firstSeen, lastSeen, err)
	}
}

func TestAppendWritesNullForAnEventWithNoInitiativeOrRef(t *testing.T) {
	st := newStore(t)
	inTx(t, st, func(tx *sql.Tx) {
		err := Append(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host"}, t0,
			Event{Principal: "key:k", Type: "ping", Summary: "s"})
		if err != nil {
			t.Fatal(err)
		}
	})
	var principal, data, kind string
	var initiative, ref sql.NullString
	err := st.Reader().QueryRowContext(context.Background(),
		`SELECT e.principal, e.initiative_id, e.ref_id, e.data, a.kind FROM events e JOIN actors a ON a.id = e.actor_id`).Scan(&principal, &initiative, &ref, &data, &kind)
	if err != nil || principal != "key:k" || initiative.Valid || ref.Valid || data != "{}" || kind != "agent" {
		t.Errorf("event %q %v %v %q %q (%v)", principal, initiative, ref, data, kind, err)
	}
}

func TestAppendFailsOnAnInitiativeThatDoesNotExist(t *testing.T) {
	st := newStore(t)
	tx, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	err = Append(context.Background(), tx, st.Dialect(), Actor{Handle: "a"}, t0, Event{Principal: "key:k", Type: "x", InitiativeID: "nope"})
	if err == nil {
		t.Error("an event of an unknown initiative is appended")
	}
}
