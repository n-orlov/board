package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // the driver the admin connection uses
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// postgresImage is the same pinned image the features' Postgres leg runs.
const postgresImage = "postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

// legs are the databases a test runs against: BOARD_DB_LEG names one, and an
// unset variable means both, as the whole-suite Go pass runs it.
func legs(t *testing.T) []string {
	t.Helper()
	switch leg := strings.TrimSpace(os.Getenv("BOARD_DB_LEG")); leg {
	case "":
		return []string{"sqlite", "postgres"}
	case "sqlite", "postgres":
		return []string{leg}
	default:
		t.Fatalf("BOARD_DB_LEG=%q: want sqlite or postgres", leg)
		return nil
	}
}

// databases hands out empty databases of one leg.
type databases interface {
	// New returns the URL of an empty database.
	New(t *testing.T) string
}

type sqliteDatabases struct{}

func (sqliteDatabases) New(t *testing.T) string {
	t.Helper()
	return "sqlite://" + filepath.Join(t.TempDir(), "b.db")
}

// postgresDatabases is one Postgres container started through the docker socket
// the way features/postgres_test.go does, with one database per call.
type postgresDatabases struct {
	host, port string
	count      atomic.Int64
}

const (
	pgUser     = "board"
	pgPassword = "board-test"
	pgAdminDB  = "postgres"
)

func pgDSN(host, port, database string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword(pgUser, pgPassword), Host: host + ":" + port, Path: "/" + database,
		RawQuery: "sslmode=disable&connect_timeout=5"}
	return u.String()
}

var (
	sharedPostgres     *postgresDatabases
	sharedPostgresDone func()
	sharedPostgresOnce sync.Once
	sharedPostgresErr  error
)

// TestMain removes the Postgres container the tests of this package share.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedPostgresDone != nil {
		sharedPostgresDone()
	}
	os.Exit(code)
}

// startPostgres returns the Postgres container the tests of this package share,
// starting it on first use.
func startPostgres(t *testing.T) *postgresDatabases {
	t.Helper()
	sharedPostgresOnce.Do(func() { sharedPostgres, sharedPostgresDone, sharedPostgresErr = runPostgres() })
	if sharedPostgresErr != nil {
		t.Fatal(sharedPostgresErr)
	}
	return sharedPostgres
}

// runPostgres starts a container through the docker socket and returns it with
// the function that removes it.
func runPostgres() (*postgresDatabases, func(), error) {
	ctx := context.Background()
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	if host := strings.TrimSpace(os.Getenv("BOARD_CI_DOCKER_HOST")); host != "" {
		_ = os.Setenv("TESTCONTAINERS_HOST_OVERRIDE", host)
	}
	labels := map[string]string{"ralphd.role": "sibling", "board.ci": "postgres"}
	if id := strings.TrimSpace(os.Getenv("BOARD_CI_RUN_ID")); id != "" {
		labels["ralphd.run"] = id
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image:        postgresImage,
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_USER": pgUser, "POSTGRES_PASSWORD": pgPassword, "POSTGRES_DB": pgAdminDB},
		Labels:       labels,
		WaitingFor: wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string { return pgDSN(host, port.Port(), pgAdminDB) }).
			WithStartupTimeout(90 * time.Second),
	}})
	terminate := func() {
		if c != nil {
			_ = c.Terminate(context.Background())
		}
	}
	if err != nil {
		terminate()
		return nil, nil, fmt.Errorf("starting Postgres through the docker socket: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		terminate()
		return nil, nil, err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		terminate()
		return nil, nil, err
	}
	return &postgresDatabases{host: host, port: port.Port()}, terminate, nil
}

func (p *postgresDatabases) New(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("board_%d_%d", os.Getpid(), p.count.Add(1))
	db, err := sql.Open("pgx", pgDSN(p.host, p.port, pgAdminDB))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating database %s: %v", name, err)
	}
	return pgDSN(p.host, p.port, name)
}

// forEachLeg runs fn as a subtest on every leg legs names, with the leg's
// databases. The Postgres container is shared by the package's tests and
// removed when they have all run.
func forEachLeg(t *testing.T, fn func(t *testing.T, leg string, dbs databases)) {
	t.Helper()
	for _, leg := range legs(t) {
		var dbs databases = sqliteDatabases{}
		if leg == "postgres" {
			dbs = startPostgres(t)
		}
		t.Run(leg, func(t *testing.T) { fn(t, leg, dbs) })
	}
}
