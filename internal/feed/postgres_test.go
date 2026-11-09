package feed

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // the driver the admin connection uses
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/events"
	"github.com/n-orlov/board/internal/store"
)

// postgresImage is the same pinned image the features' Postgres leg runs.
const postgresImage = "postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

// startPostgres starts a Postgres container through the docker socket the way
// features/postgres_test.go does and returns the URL of an empty database.
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	if host := strings.TrimSpace(os.Getenv("BOARD_CI_DOCKER_HOST")); host != "" {
		_ = os.Setenv("TESTCONTAINERS_HOST_OVERRIDE", host)
	}
	labels := map[string]string{"ralphd.role": "sibling", "board.ci": "postgres"}
	if id := strings.TrimSpace(os.Getenv("BOARD_CI_RUN_ID")); id != "" {
		labels["ralphd.run"] = id
	}
	dsn := func(host, port, db string) string {
		u := url.URL{Scheme: "postgres", User: url.UserPassword("board", "board-test"), Host: host + ":" + port, Path: "/" + db, RawQuery: "sslmode=disable&connect_timeout=5"}
		return u.String()
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image:        postgresImage,
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_USER": "board", "POSTGRES_PASSWORD": "board-test", "POSTGRES_DB": "board"},
		Labels:       labels,
		WaitingFor: wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string { return dsn(host, port.Port(), "board") }).
			WithStartupTimeout(90 * time.Second),
	}})
	if c != nil {
		t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	}
	if err != nil {
		t.Fatalf("starting Postgres through the docker socket: %v", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return dsn(host, port.Port(), "board")
}

func openPostgres(t *testing.T, url string) *store.Store {
	t.Helper()
	st, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// startFeed runs a feed on st, polling every 25 ms, and subscribes to it.
func startFeed(t *testing.T, st *store.Store) *Subscription {
	t.Helper()
	f, err := New(context.Background(), StoreSource{Store: st}, Options{Poll: 25 * time.Millisecond, GapGrace: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	s := f.Subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return s
}

// collect reads want events from s, failing on a timeout.
func collect(t *testing.T, name string, s *Subscription, want int) []events.Event {
	t.Helper()
	var got []events.Event
	for len(got) < want {
		select {
		case e, ok := <-s.Events():
			if !ok {
				t.Fatalf("%s: subscription closed after %d events: %v", name, len(got), s.Err())
			}
			got = append(got, e)
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: %d of %d events after 10 s: %v", name, len(got), want, summaries(got))
		}
	}
	return got
}

func summaries(es []events.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = fmt.Sprintf("%d:%s", e.N, e.Summary)
	}
	return out
}

func appendIn(t *testing.T, tx *sql.Tx, st *store.Store, actorID, summary string) {
	t.Helper()
	err := audit.AppendAs(context.Background(), tx, st.Dialect(), actorID, t0, audit.Event{Principal: "tester", Type: "note_posted", Summary: summary})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTwoFeedsOnOnePostgresDatabaseDeliverTheWholeSequenceInOrder(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)
	a, b := openPostgres(t, url), openPostgres(t, url)
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// The actor row first, in its own transaction, so the writers below do not
	// wait on each other for it.
	tx, err := a.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	actorID, err := audit.UpsertActor(ctx, tx, a.Dialect(), audit.Actor{Handle: "tester"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	subA, subB := startFeed(t, a), startFeed(t, b)

	appendEvent2 := func(summary string) {
		tx, err := a.Writer().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		appendIn(t, tx, a, actorID, summary)
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent2("first")
	// "slow" takes n=2 and stays open; "fast" takes n=3 and commits before it.
	slow, err := a.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	appendIn(t, slow, a, actorID, "slow")
	fast, err := b.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	appendIn(t, fast, b, actorID, "fast")
	if err := fast.Commit(); err != nil {
		t.Fatal(err)
	}

	// Both feeds see "first" but must hold "fast" back while n=2 is uncommitted.
	for name, s := range map[string]*Subscription{"a": subA, "b": subB} {
		if got := collect(t, name, s, 1); got[0].Summary != "first" {
			t.Fatalf("%s: first event %v", name, summaries(got))
		}
		select {
		case e := <-s.Events():
			t.Fatalf("%s: delivered n=%d %q ahead of the uncommitted n=2", name, e.N, e.Summary)
		case <-time.After(300 * time.Millisecond):
		}
	}
	if err := slow.Commit(); err != nil {
		t.Fatal(err)
	}
	appendEvent2("last")

	want := []string{"slow", "fast", "last"}
	for name, s := range map[string]*Subscription{"a": subA, "b": subB} {
		got := collect(t, name, s, len(want))
		for i, e := range got {
			if e.Summary != want[i] || e.N != int64(i+2) {
				t.Errorf("%s: event %d is %d:%s, want %d:%s (all: %v)", name, i, e.N, e.Summary, i+2, want[i], summaries(got))
			}
		}
		select {
		case e := <-s.Events():
			t.Errorf("%s: duplicate or extra event n=%d %q", name, e.N, e.Summary)
		case <-time.After(300 * time.Millisecond):
		}
	}
}
