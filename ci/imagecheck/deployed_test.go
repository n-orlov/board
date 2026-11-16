//go:build imagedeploy

package main

// The deployed-configuration test (SPEC 12.4): the built image started the way
// the deployment runs it, against a database written before this phase. It needs
// the image in the docker daemon and the daemon's socket, so it is behind the
// imagedeploy build tag and ci/image-check.sh runs it through ci/run.sh, after
// ci/image-build.sh has loaded board:ci:
//
//	ci/run.sh go test -tags imagedeploy -count=1 -run TestDeployedConfiguration ./ci/imagecheck/
//
// BOARD_IMAGE_REF names another image to run.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"

	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/oidc/oidctest"
	"github.com/n-orlov/village-hall/internal/store"
)

// baseSchemaVersion is the head of the SQLite schema at 0315466, the last
// release before the rename: 00009_notifications. The fixture is migrated to
// exactly it, whatever migrations the image carries on top.
const baseSchemaVersion = 9

const (
	fixtureActor       = "seed-agent"
	fixtureInitiative  = "deployed-seed"
	fixtureTaskTitle   = "A task written before the rename"
	fixtureEmail       = "operator@example.com"
	fixtureOIDCClient  = "deployed-client"
	deployedUID        = "65532:65532"
	listenAddress      = "127.0.0.1:8080"
	fixtureInitiativeT = "Deployed seed"
)

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// fixture is a SQLite file at the 0315466 schema holding an actor, an
// initiative with a task and an event, an API key and a browser session. The
// key and the session ID are random per run and only their hashes are stored,
// as the product stores them.
type fixture struct {
	path      string
	apiKey    string
	sessionID string
}

func writeFixture(t *testing.T, dir string) fixture {
	t.Helper()
	ctx := context.Background()
	key, err := auth.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{path: filepath.Join(dir, "board.db"), apiKey: key, sessionID: randomHex(t, 32)}
	st, err := store.Open("sqlite://" + f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MigrateTo(ctx, baseSchemaVersion); err != nil {
		t.Fatal(err)
	}
	if v, err := st.SchemaVersion(ctx); err != nil || v != baseSchemaVersion {
		t.Fatalf("fixture schema version = %d, %v; want %d", v, err, baseSchemaVersion)
	}
	now := time.Now().UTC().UnixMilli()
	later := time.Now().Add(24 * time.Hour).UTC().UnixMilli()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO actors (id, handle, kind, first_seen_at, last_seen_at) VALUES (?, ?, 'agent', ?, ?)`,
			[]any{"01JD0000000000000000000A01", fixtureActor, now, now}},
		{`INSERT INTO initiatives (id, slug, title, description, created_by, created_at, next_task_seq) VALUES (?, ?, ?, 'Written by the previous release.', ?, ?, 2)`,
			[]any{"01JD0000000000000000000B01", fixtureInitiative, fixtureInitiativeT, "01JD0000000000000000000A01", now}},
		{`INSERT INTO tasks (id, initiative_id, seq, title, created_by, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?, ?)`,
			[]any{"01JD0000000000000000000C01", "01JD0000000000000000000B01", fixtureTaskTitle, "01JD0000000000000000000A01", now, now}},
		{`INSERT INTO events (id, at, actor_id, principal, initiative_id, type, ref_id, summary) VALUES (?, ?, ?, 'key:seed', ?, 'initiative_created', ?, 'initiative deployed-seed created')`,
			[]any{"01JD0000000000000000000E01", now, "01JD0000000000000000000A01", "01JD0000000000000000000B01", "01JD0000000000000000000B01"}},
		{`INSERT INTO api_keys (id, name, key_hash, display_prefix, created_at, created_by) VALUES (?, 'seed-key', ?, ?, ?, 'admin-cli')`,
			[]any{"01JD0000000000000000000K01", auth.HashKey(f.apiKey), auth.DisplayPrefix(f.apiKey), now}},
		{`INSERT INTO sessions (id_hash, email, csrf_token, created_at, last_seen_at, expires_at) VALUES (?, ?, 'csrf-seed', ?, ?, ?)`,
			[]any{sha256Hex(f.sessionID), fixtureEmail, now, now, later}},
	}
	for _, s := range statements {
		if _, err := st.Writer().ExecContext(ctx, s.query, s.args...); err != nil {
			t.Fatalf("seed %q: %v", s.query, err)
		}
	}
	// One file, no -wal beside it: the container is handed the file alone.
	if _, err := st.Writer().ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(f.path + "-wal"); err == nil && info.Size() != 0 {
		t.Fatalf("%s-wal still holds %d bytes after the checkpoint", f.path, info.Size())
	}
	return f
}

// get sends one GET to the container and returns the status and body.
func get(t *testing.T, path string, header http.Header) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+listenAddress+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range header {
		req.Header[name] = values
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func withKey(key string) http.Header {
	return http.Header{"Authorization": {"Bearer " + key}, "Board-Actor": {fixtureActor}, "Accept": {"application/json"}}
}

// wrongKey is a well-formed key no row holds.
func wrongKey(t *testing.T) string {
	t.Helper()
	key, err := auth.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func withSession(id string) http.Header {
	return http.Header{"Cookie": {"board_session=" + id}, "Accept": {"application/json"}}
}

func imageRef() string {
	if image := os.Getenv("BOARD_IMAGE_REF"); image != "" {
		return image
	}
	return "board:ci"
}

// siblingLabels are put on every container the test starts, so the job that
// owns the run can reap a leaked one and nothing else can mistake it for its own.
func siblingLabels() map[string]string {
	labels := map[string]string{"ralphd.role": "sibling", "board.ci": "image-deployed"}
	if id := strings.TrimSpace(os.Getenv("BOARD_CI_RUN_ID")); id != "" {
		labels["ralphd.run"] = id
	}
	return labels
}

// seedVolume creates a volume holding the fixture as /data/board.db, owned by
// nobody in particular and writable by the container's uid, and removes it when
// the test ends. A read-only root filesystem refuses `docker cp`, so a
// container of the same image that never runs, with no read-only root, carries
// the file in; the volume's /data is the image's own, owned by 65532.
func seedVolume(t *testing.T, image string, labels map[string]string, f fixture) string {
	t.Helper()
	ctx := context.Background()
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	name := "board-image-deployed-" + randomHex(t, 6)
	docker, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = docker.VolumeRemove(ctx, name, client.VolumeRemoveOptions{Force: true})
		_ = docker.Close()
	})
	carrier, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:  image,
			Labels: labels,
			User:   deployedUID,
			Files:  []testcontainers.ContainerFile{{HostFilePath: f.path, ContainerFilePath: "/data/board.db", FileMode: 0o666}},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.Mounts = []mount.Mount{{Type: mount.TypeVolume, Source: name, Target: "/data"}}
			},
		},
		Started: false,
	})
	if carrier != nil {
		_ = carrier.Terminate(ctx)
	}
	if err != nil {
		t.Fatalf("copying the database into the volume: %v", err)
	}
	return name
}

// startDeployed starts the image with the deployed configuration. The
// container shares this process's network namespace, so the mock provider on
// this container's loopback is the issuer the container sees, and the server
// answers on the same loopback.
func startDeployed(t *testing.T, issuer, secret string, f fixture) testcontainers.Container {
	t.Helper()
	ctx := context.Background()
	self, err := os.Hostname()
	if err != nil || self == "" {
		t.Fatalf("this process has no hostname to join the network of: %v", err)
	}
	image := imageRef()
	labels := siblingLabels()
	volume := seedVolume(t, image, labels, f)
	req := testcontainers.ContainerRequest{
		Image: image,
		// command: [serve] and no entrypoint override: the image's own /vh runs it.
		Cmd: []string{"serve"},
		Env: map[string]string{
			"BOARD_LISTEN":              listenAddress,
			"BOARD_DB_URL":              "sqlite:///data/board.db",
			"BOARD_PUBLIC_URL":          "https://board.example.com",
			"BOARD_OIDC_ISSUER":         issuer,
			"BOARD_OIDC_ALLOWED_EMAILS": fixtureEmail,
			"BOARD_OIDC_CLIENT_ID":      fixtureOIDCClient,
			"BOARD_OIDC_CLIENT_SECRET":  secret,
			"BOARD_TASK":                "an-external-kits-variable-the-product-does-not-read",
		},
		Labels: labels,
		User:   deployedUID,
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.NetworkMode = container.NetworkMode("container:" + self)
			hc.ReadonlyRootfs = true
			hc.CapDrop = []string{"ALL"}
			hc.Tmpfs = map[string]string{"/tmp": "rw,noexec,nosuid,size=16m"}
			hc.Mounts = []mount.Mount{{Type: mount.TypeVolume, Source: volume, Target: "/data"}}
		},
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if c != nil {
		t.Cleanup(func() {
			if logs, lerr := c.Logs(ctx); lerr == nil {
				if data, rerr := io.ReadAll(logs); rerr == nil && t.Failed() {
					t.Logf("container log:\n%s", data)
				}
			}
			_ = c.Terminate(ctx)
		})
	}
	if err != nil {
		t.Fatalf("starting %s through the docker socket: %v", image, err)
	}
	return c
}

func waitHealthy(t *testing.T, c testcontainers.Container) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if state, err := c.State(context.Background()); err == nil && !state.Running {
			t.Fatalf("the container stopped (status %q, exit code %d) before /healthz answered", state.Status, state.ExitCode)
		}
		req, _ := http.NewRequest(http.MethodGet, "http://"+listenAddress+"/healthz", nil)
		if resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("/healthz did not answer 200 within 30s")
}

// bodyHas fails unless the body is JSON and contains want.
func bodyHas(t *testing.T, what string, body []byte, want string) {
	t.Helper()
	if !json.Valid(body) {
		t.Fatalf("%s: the body is not JSON: %.200s", what, body)
	}
	if !strings.Contains(string(body), want) {
		t.Errorf("%s: %q is not in %.400s", what, want, body)
	}
}

func TestDeployedConfigurationStartsTheImageOnAPhaseThreeDatabase(t *testing.T) {
	secret := randomHex(t, 16)
	provider, err := oidctest.New(fixtureOIDCClient, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	provider.SignIn(fixtureEmail, true)

	f := writeFixture(t, t.TempDir())
	c := startDeployed(t, provider.URL(), secret, f)
	waitHealthy(t, c)

	if code, _ := get(t, "/healthz", nil); code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", code)
	}

	// The key written before the rename authenticates, and the rows read back.
	code, body := get(t, "/api/v1/initiatives", withKey(f.apiKey))
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/initiatives with the existing brd_ key = %d, want 200: %.300s", code, body)
	}
	bodyHas(t, "initiatives", body, fixtureInitiative)
	bodyHas(t, "initiatives", body, fixtureInitiativeT)
	code, body = get(t, "/api/v1/tasks/"+fixtureInitiative+"-1", withKey(f.apiKey))
	if code != http.StatusOK {
		t.Fatalf("GET the seeded task = %d, want 200: %.300s", code, body)
	}
	bodyHas(t, "the seeded task", body, fixtureTaskTitle)
	code, body = get(t, "/api/v1/events", withKey(f.apiKey))
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/events = %d, want 200: %.300s", code, body)
	}
	bodyHas(t, "events", body, "initiative deployed-seed created")

	// The session cookie written before the rename is accepted.
	code, body = get(t, "/api/v1/initiatives", withSession(f.sessionID))
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/initiatives with the existing board_session cookie = %d, want 200: %.300s", code, body)
	}
	bodyHas(t, "initiatives by session", body, fixtureInitiative)

	// The same requests without a credential, or with a wrong one, are refused:
	// the 200s above are the credentials' doing.
	refused := map[string]http.Header{
		"no credential":   {"Accept": {"application/json"}},
		"a wrong key":     withKey(wrongKey(t)),
		"a wrong session": withSession(randomHex(t, 32)),
	}
	for name, header := range refused {
		if code, _ := get(t, "/api/v1/initiatives", header); code != http.StatusUnauthorized {
			t.Errorf("GET /api/v1/initiatives with %s = %d, want 401", name, code)
		}
	}

	inspect, err := c.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hc := inspect.HostConfig; hc == nil || !hc.ReadonlyRootfs || len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" {
		t.Errorf("the container did not run with a read-only root and every capability dropped: %+v", hc)
	}
	if inspect.Config == nil || inspect.Config.User != deployedUID || len(inspect.Config.Entrypoint) != 1 || inspect.Config.Entrypoint[0] != wantEntrypoint {
		t.Errorf("the container ran as %+v, want user %s and the image's own entrypoint %s", inspect.Config, deployedUID, wantEntrypoint)
	}
}
