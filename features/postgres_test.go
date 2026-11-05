package features

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // the driver the Postgres leg's admin connection uses
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// postgresImage is the Postgres the leg runs against, pinned by digest (16 is
// the oldest version SPEC 12.2 supports).
const postgresImage = "postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab"

const (
	pgUser     = "board"
	pgPassword = "board-test"
	pgAdminDB  = "postgres"
)

// siblingLabels are put on every container the suite starts, so the job that
// owns this run can reap them and nothing else can mistake them for its own.
// ci/run.sh forwards the run id as BOARD_CI_RUN_ID.
func siblingLabels() map[string]string {
	labels := map[string]string{"ralphd.role": "sibling", "board.ci": "postgres"}
	if id := strings.TrimSpace(os.Getenv("BOARD_CI_RUN_ID")); id != "" {
		labels["ralphd.run"] = id
	}
	return labels
}

// configureTestcontainers points testcontainers at the docker host the way
// ci/run.sh set the sibling up. The reaper container is off because the harness
// removes each container it starts itself and every container carries the
// sibling labels, so the job that owns the run can reap a leaked one.
func configureTestcontainers() {
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	if host := strings.TrimSpace(os.Getenv("BOARD_CI_DOCKER_HOST")); host != "" {
		_ = os.Setenv("TESTCONTAINERS_HOST_OVERRIDE", host)
	}
}

// postgresServer is one Postgres container started through the docker socket.
type postgresServer struct {
	container testcontainers.Container
	host      string
	port      string
}

// startPostgres starts a Postgres container and waits until it accepts SQL.
// The container is reached on the docker host's published port, which
// ci/run.sh makes resolvable from the sibling.
func startPostgres(ctx context.Context) (*postgresServer, error) {
	configureTestcontainers()
	req := testcontainers.ContainerRequest{
		Image:        postgresImage,
		ExposedPorts: []string{"5432/tcp"},
		Env:          map[string]string{"POSTGRES_USER": pgUser, "POSTGRES_PASSWORD": pgPassword, "POSTGRES_DB": pgAdminDB},
		Labels:       siblingLabels(),
		WaitingFor:   wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string { return adminDSN(host, port.Port()) }).WithStartupTimeout(90 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		if c != nil {
			_ = c.Terminate(ctx)
		}
		return nil, fmt.Errorf("starting Postgres through the docker socket: %w", err)
	}
	host, err := c.Host(ctx)
	if err == nil {
		var port string
		if p, perr := c.MappedPort(ctx, "5432/tcp"); perr != nil {
			err = perr
		} else {
			port = p.Port()
		}
		if err == nil {
			return &postgresServer{container: c, host: host, port: port}, nil
		}
	}
	_ = c.Terminate(ctx)
	return nil, err
}

func adminDSN(host, port string) string {
	return dsn(host, port, pgAdminDB)
}

func dsn(host, port, database string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(pgUser, pgPassword),
		Host:     host + ":" + port,
		Path:     "/" + database,
		RawQuery: "sslmode=disable&connect_timeout=5",
	}
	return u.String()
}

var databaseCounter atomic.Int64

// newDatabase creates an empty database on the server and returns its URL.
func (p *postgresServer) newDatabase(ctx context.Context) (string, error) {
	name := fmt.Sprintf("board_%d_%d", os.Getpid(), databaseCounter.Add(1))
	db, err := sql.Open("pgx", adminDSN(p.host, p.port))
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		return "", fmt.Errorf("creating database %s: %w", name, err)
	}
	return dsn(p.host, p.port, name), nil
}

// stop stops the container, the way an outage of the database would.
func (p *postgresServer) stop(ctx context.Context) error {
	timeout := 10 * time.Second
	return p.container.Stop(ctx, &timeout)
}

// terminate removes the container.
func (p *postgresServer) terminate(ctx context.Context) {
	_ = p.container.Terminate(ctx)
}
