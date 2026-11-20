package audit

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/store"
)

type batchActors struct{ op, agent, ownerless string }

// batchWorld has an operator, an agent that took the operator as its origin and
// an agent with none.
func batchWorld(t *testing.T) (batchActors, func(context.Context, []Written) []string, func(string) (string, string), *store.Store) {
	t.Helper()
	st := newStore(t)
	d := st.Dialect()
	bg := context.Background()
	var a batchActors
	inTx(t, st, func(tx *sql.Tx) {
		var err error
		if a.op, err = UpsertActor(bg, tx, d, Actor{Handle: "alice-op", Operator: true}, t0); err != nil {
			t.Fatal(err)
		}
		if a.agent, err = UpsertActor(WithOrigin(bg, "alice-op"), tx, d, Actor{Handle: "claude"}, t0); err != nil {
			t.Fatal(err)
		}
		if a.ownerless, err = UpsertActor(WithOrigin(bg, ""), tx, d, Actor{Handle: "codex"}, t0); err != nil {
			t.Fatal(err)
		}
	})
	write := func(ctx context.Context, events []Written) []string {
		var ids []string
		inTx(t, st, func(tx *sql.Tx) {
			var err error
			if ids, err = AppendBatch(ctx, tx, d, t0.Add(time.Minute), events); err != nil {
				t.Fatal(err)
			}
		})
		return ids
	}
	// read gives the handle of an event's actor and origin ("" for none), by summary.
	read := func(summary string) (string, string) {
		var actor string
		var origin sql.NullString
		err := st.Reader().QueryRow(`SELECT a.handle, o.handle FROM events e JOIN actors a ON a.id = e.actor_id
			LEFT JOIN actors o ON o.id = e.origin WHERE e.summary = ?`, summary).Scan(&actor, &origin)
		if err != nil {
			t.Fatal(err)
		}
		return actor, origin.String
	}
	return a, write, read, st
}

func TestABatchOfEventsTakesEachActorRowsOwnOriginOutsideARequest(t *testing.T) {
	a, write, read, _ := batchWorld(t)
	write(context.Background(), []Written{
		{ActorID: a.op, ActorOrigin: a.op, Event: Event{Principal: "system:expiry", Type: "t", Summary: "op"}},
		{ActorID: a.agent, ActorOrigin: a.op, Event: Event{Principal: "system:expiry", Type: "t", Summary: "agent"}},
		{ActorID: a.ownerless, Event: Event{Principal: "system:expiry", Type: "t", Summary: "ownerless"}},
	})
	for summary, want := range map[string][2]string{"op": {"alice-op", "alice-op"}, "agent": {"claude", "alice-op"}, "ownerless": {"codex", ""}} {
		if actor, origin := read(summary); actor != want[0] || origin != want[1] {
			t.Errorf("%s: actor %q origin %q, want %q and %q", summary, actor, origin, want[0], want[1])
		}
	}
}

func TestABatchOfEventsTakesTheOriginTheRequestCarries(t *testing.T) {
	a, write, read, _ := batchWorld(t)
	for name, tc := range map[string]struct {
		ctx  context.Context
		want string
	}{
		"the operator the request names": {WithOrigin(context.Background(), "alice-op"), "alice-op"},
		"an empty origin is none":        {WithOrigin(context.Background(), ""), ""},
		"a handle of no operator":        {WithOrigin(context.Background(), "nobody"), ""},
	} {
		summary := "by " + name
		write(tc.ctx, []Written{{ActorID: a.agent, ActorOrigin: a.op, Event: Event{Principal: "key:k", Type: "t", Summary: summary}}})
		if _, origin := read(summary); origin != tc.want {
			t.Errorf("%s: origin %q, want %q", name, origin, tc.want)
		}
	}
}

func TestABatchOfEventsWritesEveryColumnAndReturnsTheIDsInOrder(t *testing.T) {
	a, write, _, st := batchWorld(t)
	ids := write(context.Background(), []Written{
		{ActorID: a.agent, Event: Event{Principal: "system:expiry", Type: "lease_expired", Summary: "first", RefID: "", Data: map[string]any{"ref": "L1"}}},
		{ActorID: a.agent, Event: Event{Principal: "system:expiry", Type: "lease_expired", Summary: "second", OnBehalfOfID: a.op}},
	})
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("ids = %v", ids)
	}
	var typ, principal, data, summary string
	var onBehalf sql.NullString
	row := func(id string) {
		t.Helper()
		if err := st.Reader().QueryRow(`SELECT type, principal, data, summary, on_behalf_of FROM events WHERE id = ?`, id).
			Scan(&typ, &principal, &data, &summary, &onBehalf); err != nil {
			t.Fatal(err)
		}
	}
	if row(ids[0]); typ != "lease_expired" || principal != "system:expiry" || data != `{"ref":"L1"}` || summary != "first" || onBehalf.Valid {
		t.Errorf("first = %s %s %s %s %v", typ, principal, data, summary, onBehalf)
	}
	if row(ids[1]); data != `{}` || summary != "second" || onBehalf.String != a.op {
		t.Errorf("second = %s %s %v, want an empty object and its on-behalf actor", data, summary, onBehalf)
	}
	if got := write(context.Background(), nil); got != nil {
		t.Errorf("a batch of no events returned ids %v", got)
	}
}

func TestABatchOfEventsWritesMoreThanOneStatementsWorth(t *testing.T) {
	a, write, _, _ := batchWorld(t)
	events := make([]Written, 2*store.InsertManyRows+1)
	for i := range events {
		events[i] = Written{ActorID: a.agent, Event: Event{Principal: "system:expiry", Type: "t", Summary: "e" + strconv.Itoa(i)}}
	}
	ids := write(context.Background(), events)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(ids) != len(events) || len(seen) != len(events) {
		t.Fatalf("%d ids (%d distinct) for %d events", len(ids), len(seen), len(events))
	}
}

func TestABatchOfEventsRefusedWritesNone(t *testing.T) {
	st := newStore(t)
	tx, err := st.Writer().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	actorID, err := UpsertActor(context.Background(), tx, st.Dialect(), Actor{Handle: "claude"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := AppendBatch(context.Background(), tx, st.Dialect(), t0, []Written{
		{ActorID: actorID, Event: Event{Principal: "k", Type: "t", Summary: "s"}},
		{ActorID: actorID, Event: Event{Principal: "k", Type: "t", InitiativeID: "no-such", Summary: "s"}},
	})
	if err == nil || ids != nil {
		t.Fatalf("ids = %v, err = %v, want an error and no ids", ids, err)
	}
}
