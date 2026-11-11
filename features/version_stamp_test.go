package features

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	stampedVersion  = "v7.6.5-stamped"
	stampedRevision = "0f1e2d3c4b5a69788796a5b4c3d2e1f001122334"
	versionPackage  = "github.com/n-orlov/board/internal/version"
)

// TestLinkerStampedVersionAndRevisionReachTheBinaryAndTheAPI builds the board
// binary the way the Dockerfile does, with -ldflags -X for the version and the
// revision (no VCS info: -buildvcs=false, as in the image's build context), and
// asks the built binary: `board version` prints both, and GET /api/v1/meta of
// its server reports both. A build that reads build info alone cannot pass.
func TestLinkerStampedVersionAndRevisionReachTheBinaryAndTheAPI(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "board")
	ldflags := "-X " + versionPackage + ".version=" + stampedVersion + " -X " + versionPackage + ".revision=" + stampedRevision
	build := exec.Command("go", "build", "-buildvcs=false", "-ldflags", ldflags, "-o", bin, boardPackage)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stamped binary: %v\n%s", err, out)
	}

	h := &harness{binary: bin, leg: "sqlite"}
	t.Cleanup(h.stopServer)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := h.runArgs(ctx, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if want := stampedVersion + " " + stampedRevision + "\n"; h.stdout != want || h.exit != 0 {
		t.Fatalf("board version: exit %d, stdout %q, want %q", h.exit, h.stdout, want)
	}

	if err := h.freshDatabase(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"admin", "migrate"}, {"admin", "keys", "create", "stamp"}} {
		if err := h.runArgs(ctx, args); err != nil || h.exit != 0 {
			t.Fatalf("board %v: %v, exit %d, stderr %q", args, err, h.exit, h.stderr)
		}
	}
	if err := h.rememberKey(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.startServer(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sendKeyed(ctx, http.MethodGet, "/api/v1/meta", h.key, "stamp@test", nil); err != nil {
		t.Fatal(err)
	}
	doc, err := h.bodyMap()
	if err != nil {
		t.Fatal(err)
	}
	if got := doc["version"]; got != stampedVersion {
		t.Errorf("GET /api/v1/meta version = %v, want %q", got, stampedVersion)
	}
	if got := doc["revision"]; got != stampedRevision {
		t.Errorf("GET /api/v1/meta revision = %v, want %q (body %s)", got, stampedRevision, strings.TrimSpace(string(h.api.body)))
	}
}
