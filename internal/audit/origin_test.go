package audit

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// originOf reads the origin handle ("" for null) of the actor or event.
func originOf(t *testing.T, st interface {
	Reader() *sql.DB
}, q, arg string) string {
	t.Helper()
	var got sql.NullString
	if err := st.Reader().QueryRow(q, arg).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got.String
}

const (
	actorOriginQuery = `SELECT o.handle FROM actors a LEFT JOIN actors o ON o.id = a.origin WHERE a.handle = ?`
	eventOriginQuery = `SELECT o.handle FROM events e LEFT JOIN actors o ON o.id = e.origin WHERE e.summary = ?`
)

func TestOriginIsCarriedByTheContext(t *testing.T) {
	if _, ok := OriginFrom(context.Background()); ok {
		t.Error("a bare context carries an origin")
	}
	if h, ok := OriginFrom(WithOrigin(context.Background(), "alice-op")); !ok || h != "alice-op" {
		t.Errorf("OriginFrom = %q, %v, want alice-op", h, ok)
	}
	if h, ok := OriginFrom(WithOrigin(context.Background(), "")); !ok || h != "" {
		t.Errorf("an empty origin = %q, %v, want one that is carried and empty", h, ok)
	}
}

func TestAgentRowsTakeTheRequestsOriginAndEventsKeepTheirOwn(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	bg := context.Background()
	write := func(ctx context.Context, actor Actor, summary string, at time.Time) {
		inTx(t, st, func(tx *sql.Tx) {
			if err := Append(ctx, tx, d, actor, at, Event{Principal: "key:k", Type: "t", Summary: summary}); err != nil {
				t.Fatal(err)
			}
		})
	}
	write(bg, Actor{Handle: "alice-op", Operator: true}, "alice signs in", t0)
	write(bg, Actor{Handle: "bob-op", Operator: true}, "bob signs in", t0)
	write(WithOrigin(bg, "alice-op"), Actor{Handle: "claude"}, "first", t0)
	write(WithOrigin(bg, "bob-op"), Actor{Handle: "claude"}, "second", t0.Add(time.Second))
	if got := originOf(t, st, actorOriginQuery, "claude"); got != "bob-op" {
		t.Errorf("the agent's origin = %q, want its latest write's bob-op", got)
	}
	if first, second := originOf(t, st, eventOriginQuery, "first"), originOf(t, st, eventOriginQuery, "second"); first != "alice-op" || second != "bob-op" {
		t.Errorf("event origins = %q and %q, want alice-op and bob-op", first, second)
	}
	// An operator row is never given an origin, whatever key acts as it.
	write(WithOrigin(bg, "bob-op"), Actor{Handle: "alice-op"}, "alice's handle through bob's key", t0)
	if got := originOf(t, st, actorOriginQuery, "alice-op"); got != "" {
		t.Errorf("an operator row's origin = %q, want none", got)
	}
}

func TestARequestWithoutAKnownOperatorWritesANullOrigin(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	bg := context.Background()
	inTx(t, st, func(tx *sql.Tx) {
		if err := Append(WithOrigin(bg, "alice-op"), tx, d, Actor{Handle: "claude"}, t0, Event{Principal: "key:k", Type: "t", Summary: "named but unknown"}); err != nil {
			t.Fatal(err)
		}
		if err := Append(WithOrigin(bg, ""), tx, d, Actor{Handle: "claude"}, t0, Event{Principal: "key:k", Type: "t", Summary: "ownerless"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, s := range []string{"named but unknown", "ownerless"} {
		if got := originOf(t, st, eventOriginQuery, s); got != "" {
			t.Errorf("%q origin = %q, want none", s, got)
		}
	}
	if got := originOf(t, st, actorOriginQuery, "claude"); got != "" {
		t.Errorf("an origin naming no operator reached the agent row: %q", got)
	}
}

func TestAnOwnerlessWriteClearsTheAgentsEarlierOrigin(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	bg := context.Background()
	write := func(ctx context.Context, summary string) {
		inTx(t, st, func(tx *sql.Tx) {
			if err := Append(ctx, tx, d, Actor{Handle: "claude"}, t0, Event{Principal: "key:k", Type: "t", Summary: summary}); err != nil {
				t.Fatal(err)
			}
		})
	}
	inTx(t, st, func(tx *sql.Tx) {
		if _, err := UpsertActor(bg, tx, d, Actor{Handle: "alice-op", Operator: true}, t0); err != nil {
			t.Fatal(err)
		}
	})
	for _, step := range []struct{ origin, summary, want string }{
		{"alice-op", "through alice's key", "alice-op"},
		{"", "through an ownerless key", ""},
		{"alice-op", "through alice's key again", "alice-op"},
		{"ghost", "through a key whose owner is no operator", ""},
	} {
		write(WithOrigin(bg, step.origin), step.summary)
		if got := originOf(t, st, actorOriginQuery, "claude"); got != step.want {
			t.Errorf("after a write %s the agent's origin = %q, want %q", step.summary, got, step.want)
		}
		if got := originOf(t, st, eventOriginQuery, step.summary); got != step.want {
			t.Errorf("the event written %s has origin %q, want %q", step.summary, got, step.want)
		}
	}
	// A write outside any request leaves the agent's origin as its latest keyed write left it.
	write(WithOrigin(bg, "alice-op"), "keyed")
	write(bg, "outside a request")
	if got := originOf(t, st, actorOriginQuery, "claude"); got != "alice-op" {
		t.Errorf("a write outside a request changed the agent's origin to %q", got)
	}
}

func TestOnBehalfRowsAndWritesOutsideARequestFollowTheActorRow(t *testing.T) {
	st := newStore(t)
	d := st.Dialect()
	bg := context.Background()
	var claudeID, opID string
	inTx(t, st, func(tx *sql.Tx) {
		var err error
		if opID, err = UpsertActor(bg, tx, d, Actor{Handle: "alice-op", Operator: true}, t0); err != nil {
			t.Fatal(err)
		}
		ctx := WithOrigin(bg, "alice-op")
		if claudeID, err = UpsertActor(ctx, tx, d, Actor{Handle: "claude"}, t0); err != nil {
			t.Fatal(err)
		}
		// The actor a write is made on behalf of is not the one whose key wrote.
		if _, err = OnBehalf(ctx, tx, d, Actor{OnBehalfOf: "delegate"}, t0); err != nil {
			t.Fatal(err)
		}
	})
	if got := originOf(t, st, actorOriginQuery, "delegate"); got != "" {
		t.Errorf("the on-behalf actor's origin = %q, want none", got)
	}
	inTx(t, st, func(tx *sql.Tx) {
		// No request: the agent's own origin, and an operator's own id.
		if err := AppendAs(bg, tx, d, claudeID, t0, Event{Principal: "system", Type: "t", Summary: "agent, no request"}); err != nil {
			t.Fatal(err)
		}
		if err := AppendAs(bg, tx, d, opID, t0, Event{Principal: "system", Type: "t", Summary: "operator, no request"}); err != nil {
			t.Fatal(err)
		}
	})
	if got := originOf(t, st, eventOriginQuery, "agent, no request"); got != "alice-op" {
		t.Errorf("origin of a write outside a request by an agent = %q, want its latest alice-op", got)
	}
	if got := originOf(t, st, eventOriginQuery, "operator, no request"); got != "alice-op" {
		t.Errorf("origin of a write outside a request by an operator = %q, want itself", got)
	}
}
