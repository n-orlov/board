package features

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cucumber/godog"
)

// publishedVH is the content of the vh binary the stub release holds.
const publishedVH = "the published vh binary\n"

// installStamped builds the real vh binary with the version stamped in, the way
// the release build does, and puts it in a fresh directory of its own, so that
// `vh update` can replace it.
func (h *harness) installStamped(version string) error {
	dir, err := os.MkdirTemp("", "vh-update-")
	if err != nil {
		return err
	}
	bin := filepath.Join(dir, "vh")
	args := boardBuildArgs(bin, os.Getenv("GOCOVERDIR"), os.Getenv("BOARD_FEATURES_COVERMODE"))
	ldflags := "-X " + versionPackage + ".version=" + version
	args = append([]string{args[0], "-buildvcs=false", "-ldflags", ldflags}, args[1:]...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("building the stamped binary: %w\n%s", err, out)
	}
	h.api.installedVH = bin
	return nil
}

// publishRelease starts a release server for example/board holding one release
// of this platform, whose archive holds publishedVH.
func (h *harness) publishRelease(tag, immutability string) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Name: "vh", Mode: 0o755, Size: int64(len(publishedVH)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, _ = tw.Write([]byte(publishedVH))
	if err := tw.Close(); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	asset := "vh_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	immutable := immutability == "immutable"
	h.api.releases = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/example/board/releases/tags/") && !strings.HasSuffix(r.URL.Path, "/"+tag):
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/repos/example/board/releases/"):
			_, _ = fmt.Fprintf(w, `{"immutable":%t,"tag_name":%q}`, immutable, tag)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	return nil
}

// runInstalledVH runs the stamped binary against the stub release server.
func (h *harness) runInstalledVH(ctx context.Context, line string) error {
	if h.api.installedVH == "" || h.api.releases == nil {
		return fmt.Errorf("no stamped vh binary or no release server was set up")
	}
	saved := h.binary
	h.binary = h.api.installedVH
	defer func() { h.binary = saved }()
	return h.runArgs(ctx, strings.Fields(line),
		"VH_REPO=example/board", "VH_API_URL="+h.api.releases.URL, "VH_DOWNLOAD_URL="+h.api.releases.URL)
}

// runDevVH runs the binary every other scenario drives, a checkout build whose
// version names no release, against the stub release server.
func (h *harness) runDevVH(ctx context.Context, line string) error {
	if h.api.releases == nil {
		return fmt.Errorf("no release server was set up")
	}
	return h.runArgs(ctx, strings.Fields(line),
		"VH_REPO=example/board", "VH_API_URL="+h.api.releases.URL, "VH_DOWNLOAD_URL="+h.api.releases.URL)
}

func (h *harness) installedVHHolds(want string) error {
	got, err := os.ReadFile(h.api.installedVH)
	if err != nil {
		return err
	}
	if want == "the published binary" {
		want = publishedVH
		if string(got) != want {
			return fmt.Errorf("the installed vh holds %d bytes that are not the published binary", len(got))
		}
		return nil
	}
	if string(got) == publishedVH {
		return fmt.Errorf("the installed vh was replaced")
	}
	return nil
}

func (h *harness) installedVHHasMode(mode string) error {
	fi, err := os.Stat(h.api.installedVH)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("%04o", fi.Mode().Perm()); got != mode {
		return fmt.Errorf("the installed vh has mode %s, want %s", got, mode)
	}
	return nil
}

func (h *harness) stderrIsOneRefusal(want string) error {
	if strings.Count(h.stderr, "\n") != 1 || !strings.Contains(h.stderr, want) || !strings.Contains(h.stderr, " → ") {
		return fmt.Errorf("stderr %q, want one line holding %q and →", h.stderr, want)
	}
	return nil
}

func registerUpdateSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^the vh binary stamped as version "([^"]*)" is installed in its own directory$`, h.installStamped)
	sc.Step(`^a release server publishes "([^"]*)" of example/board as (immutable|not immutable)$`, h.publishRelease)
	sc.Step(`^I run the installed vh '([^']*)' against the release server$`, h.runInstalledVH)
	sc.Step(`^I run the checkout build of vh '([^']*)' against the release server$`, h.runDevVH)
	sc.Step(`^the installed vh (holds the published binary|is unchanged)$`, func(what string) error {
		if what == "is unchanged" {
			return h.installedVHHolds("unchanged")
		}
		return h.installedVHHolds("the published binary")
	})
	sc.Step(`^the installed vh has mode (\d{4})$`, h.installedVHHasMode)
	sc.Step(`^stderr is one line holding "([^"]*)" and an arrow$`, h.stderrIsOneRefusal)
}
