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

func TestUpsertActorKeepsWayBackAndMetaUnlessANewOneIsSent(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	for _, a := range []Actor{
		{Handle: "a@h", WayBack: "cmd one", Meta: `{"k":1}`},
		{Handle: "a@h"},
		{Handle: "b@h"},
		{Handle: "a@h", WayBack: "  "},
	} {
		inTx(t, st, func(tx *sql.Tx) {
			if _, err := UpsertActor(context.Background(), tx, d, a, t0); err != nil {
				t.Fatal(err)
			}
		})
	}
	for handle, want := range map[string][2]string{"a@h": {"cmd one", `{"k":1}`}, "b@h": {"", "{}"}} {
		var wb sql.NullString
		var meta string
		if err := st.Reader().QueryRowContext(context.Background(), `SELECT way_back, meta FROM actors WHERE handle = ?`, handle).Scan(&wb, &meta); err != nil {
			t.Fatal(err)
		}
		if wb.String != want[0] || wb.Valid != (want[0] != "") || meta != want[1] {
			t.Errorf("%s way_back=%v meta=%q, want %v", handle, wb, meta, want)
		}
	}
}

func TestOperatorRowIsCreatedByTheFirstOperatorWriteAndOnlyOneExists(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	count := func() (n int) {
		if err := st.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM actors WHERE kind = 'operator'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 0 {
		t.Fatal("a fresh database has an operator row before any write")
	}
	inTx(t, st, func(tx *sql.Tx) {
		if _, err := UpsertActor(context.Background(), tx, d, Actor{Handle: "operator", Operator: true}, t0); err != nil {
			t.Fatal(err)
		}
	})
	if count() != 1 {
		t.Fatal("the first operator write did not create the operator row")
	}
	tx, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := UpsertActor(context.Background(), tx, d, Actor{Handle: "other-operator", Operator: true}, t0); err == nil {
		t.Error("a second operator row was accepted")
	}
}

func kindOf(t *testing.T, st *store.Store, handle string) string {
	t.Helper()
	var kind string
	if err := st.Reader().QueryRowContext(context.Background(), `SELECT kind FROM actors WHERE handle = ?`, handle).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	return kind
}

func upsertAll(t *testing.T, st *store.Store, as ...Actor) {
	t.Helper()
	for i, a := range as {
		inTx(t, st, func(tx *sql.Tx) {
			if _, err := UpsertActor(context.Background(), tx, st.Dialect(), a, t0.Add(time.Duration(i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSessionWriteAfterKeyWriteOfTheSameHandlePromotesItToOperator(t *testing.T) {
	st := newStore(t)
	upsertAll(t, st, Actor{Handle: "operator"}, Actor{Handle: "operator", Operator: true})
	if got := kindOf(t, st, "operator"); got != "operator" {
		t.Errorf("kind after key then session = %q", got)
	}
}

func TestKeyWriteAfterSessionWriteOfTheSameHandleKeepsOperator(t *testing.T) {
	st := newStore(t)
	upsertAll(t, st, Actor{Handle: "operator", Operator: true}, Actor{Handle: "operator"}, Actor{Handle: "operator"})
	if got := kindOf(t, st, "operator"); got != "operator" {
		t.Errorf("kind after session then keys = %q", got)
	}
}

func TestPromotionTouchesOnlyTheSessionsHandleAndKeepsOtherAgents(t *testing.T) {
	st := newStore(t)
	upsertAll(t, st, Actor{Handle: "a1"}, Actor{Handle: "operator"}, Actor{Handle: "operator", Operator: true}, Actor{Handle: "a1"}, Actor{Handle: "operator", Operator: true})
	if got := kindOf(t, st, "a1"); got != "agent" {
		t.Errorf("other agent kind = %q", got)
	}
	var n int
	if err := st.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM actors WHERE kind = 'operator'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("operator rows = %d (%v)", n, err)
	}
}

func TestOnBehalfUpsertsTheNamedActorAndAppendStoresItOnTheEvent(t *testing.T) {
	st := newStore(t)
	inTx(t, st, func(tx *sql.Tx) {
		none, err := OnBehalf(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host"}, t0)
		if err != nil || none != "" {
			t.Fatalf("no header = %q (%v), want no actor", none, err)
		}
		blank, err := OnBehalf(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host", OnBehalfOf: "  "}, t0)
		if err != nil || blank != "" {
			t.Fatalf("blank header = %q (%v), want no actor", blank, err)
		}
		id, err := OnBehalf(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host", OnBehalfOf: " operator "}, t0)
		if err != nil || id == "" {
			t.Fatalf("OnBehalf = %q (%v)", id, err)
		}
		if err := Append(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host"}, t0,
			Event{Principal: "key:k", Type: "ping", Summary: "s", OnBehalfOfID: id}); err != nil {
			t.Fatal(err)
		}
	})
	var handle string
	err := st.Reader().QueryRowContext(context.Background(),
		`SELECT ob.handle FROM events e JOIN actors ob ON ob.id = e.on_behalf_of`).Scan(&handle)
	if err != nil || handle != "operator" {
		t.Errorf("on_behalf_of = %q (%v), want operator", handle, err)
	}
}

func TestAppendAsIDReturnsTheIDOfTheEventItWrote(t *testing.T) {
	st := newStore(t)
	var id string
	inTx(t, st, func(tx *sql.Tx) {
		actorID, err := UpsertActor(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host"}, t0)
		if err != nil {
			t.Fatal(err)
		}
		if id, err = AppendAsID(context.Background(), tx, st.Dialect(), actorID, t0, Event{Principal: "key:x", Type: "note_posted", Summary: "s"}); err != nil {
			t.Fatal(err)
		}
	})
	var typ string
	if err := st.Reader().QueryRowContext(context.Background(), st.Dialect().Rebind(`SELECT type FROM events WHERE id = ?`), id).Scan(&typ); err != nil || typ != "note_posted" {
		t.Fatalf("event %q has type %q (%v)", id, typ, err)
	}
}

func TestAppendAsIDReturnsNoIDWhenTheEventIsRefused(t *testing.T) {
	st := newStore(t)
	tx, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	actorID, err := UpsertActor(context.Background(), tx, st.Dialect(), Actor{Handle: "claude@host"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	id, err := AppendAsID(context.Background(), tx, st.Dialect(), actorID, t0, Event{Principal: "key:x", Type: "t", InitiativeID: "no-such", Summary: "s"})
	if err == nil || id != "" {
		t.Fatalf("id = %q, err = %v, want no id and an error", id, err)
	}
}
