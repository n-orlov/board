//go:build releasebuild

package repocheck

// The release dry run's assertions (SPEC 12.5, R11): what ci/release-build.sh
// left in its output directory. The build needs the docker daemon and the CI
// agent, so this is behind the releasebuild build tag and ci/release-check.sh
// builds into a directory and runs it through ci/run.sh:
//
//	BOARD_RELEASE_OUT=/w/ci-results/release-dryrun BOARD_RELEASE_VERSION=v0.0.0-dryrun \
//	  go test -tags releasebuild -count=1 -run TestReleaseBuild ./ci/repocheck/

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var releaseTargets = []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"}

func releaseOut(t *testing.T) (dir, version string) {
	t.Helper()
	dir, version = os.Getenv("BOARD_RELEASE_OUT"), os.Getenv("BOARD_RELEASE_VERSION")
	if dir == "" || version == "" {
		t.Fatal("BOARD_RELEASE_OUT and BOARD_RELEASE_VERSION name the release directory and its version (ci/release-check.sh sets them)")
	}
	return dir, version
}

func readTarGz(t *testing.T, path string) map[string][]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("%s is not gzip: %v", path, err)
	}
	return readTar(t, path, zr)
}

func readTar(t *testing.T, name string, r io.Reader) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatalf("%s is not a tar: %v", name, err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[strings.TrimPrefix(h.Name, "./")] = body
	}
}

// TestReleaseBuildProducesTheFourTarballs: each holds exactly one executable vh.
func TestReleaseBuildProducesTheFourTarballs(t *testing.T) {
	dir, _ := releaseOut(t)
	for _, target := range releaseTargets {
		path := filepath.Join(dir, "vh_"+target+".tar.gz")
		files := readTarGz(t, path)
		if len(files) != 1 || len(files["vh"]) == 0 {
			t.Errorf("%s holds %d files, want exactly a non-empty vh", path, len(files))
		}
	}
}

// TestReleaseBuildChecksumsVerify: checksums.txt is `<sha256>  <file>` lines, covers
// every asset (every file at the top of the directory but itself) and each hash matches.
func TestReleaseBuildChecksumsVerify(t *testing.T) {
	dir, _ := releaseOut(t)
	raw, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`^([0-9a-f]{64})  ([^/ ]+)$`)
	listed := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			t.Errorf("checksums.txt line %q is not `<sha256>  <file>`", l)
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, m[2]))
		if err != nil {
			t.Errorf("checksums.txt lists %s: %v", m[2], err)
			continue
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != m[1] {
			t.Errorf("%s: checksums.txt says %s, the file is %s", m[2], m[1], got)
		}
		listed[m[2]] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var assets []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "checksums.txt" {
			assets = append(assets, e.Name())
			if !listed[e.Name()] {
				t.Errorf("checksums.txt does not cover the asset %s", e.Name())
			}
		}
	}
	sort.Strings(assets)
	for _, want := range []string{"INSTALL-AGENT.md", "install.sh", "vh_linux_amd64.tar.gz", "vh_darwin_arm64.tar.gz"} {
		if i := sort.SearchStrings(assets, want); i == len(assets) || assets[i] != want {
			t.Errorf("%s is not among the assets %v", want, assets)
		}
	}
}

// TestReleaseBuildLinuxAmd64PrintsTheVersion runs the shipped binary.
func TestReleaseBuildLinuxAmd64PrintsTheVersion(t *testing.T) {
	dir, version := releaseOut(t)
	files := readTarGz(t, filepath.Join(dir, "vh_linux_amd64.tar.gz"))
	bin := filepath.Join(t.TempDir(), "vh")
	if err := os.WriteFile(bin, files["vh"], 0o700); err != nil { //nolint:gosec // G306: the test executes this file
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "version").CombinedOutput() //nolint:gosec // G204: the binary the release build produced
	if err != nil {
		t.Fatalf("vh version: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), version) {
		t.Errorf("vh version printed %q, want it to contain %q", out, version)
	}
}

// TestReleaseBuildSBOMsParse: one CycloneDX document per tarball and one for the image.
func TestReleaseBuildSBOMsParse(t *testing.T) {
	dir, _ := releaseOut(t)
	names := []string{"village-hall-image.cdx.json"}
	for _, target := range releaseTargets {
		names = append(names, "vh_"+target+".cdx.json")
	}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var bom struct {
			BomFormat   string            `json:"bomFormat"`
			SpecVersion string            `json:"specVersion"`
			Components  []json.RawMessage `json:"components"`
		}
		if err := json.Unmarshal(raw, &bom); err != nil {
			t.Errorf("%s does not parse: %v", name, err)
			continue
		}
		if bom.BomFormat != "CycloneDX" || bom.SpecVersion == "" || len(bom.Components) == 0 {
			t.Errorf("%s: bomFormat %q, specVersion %q, %d components; want CycloneDX with components", name, bom.BomFormat, bom.SpecVersion, len(bom.Components))
		}
	}
}

// TestReleaseBuildOCIIndexListsBothPlatforms: the layout's image index names
// village-hall and lists linux/amd64 and linux/arm64.
func TestReleaseBuildOCIIndexListsBothPlatforms(t *testing.T) {
	dir, version := releaseOut(t)
	raw, err := os.ReadFile(filepath.Join(dir, "image", "board-image-oci.tar"))
	if err != nil {
		t.Fatal(err)
	}
	files := readTar(t, "the OCI layout", bytes.NewReader(raw))
	type descriptor struct {
		MediaType   string            `json:"mediaType"`
		Digest      string            `json:"digest"`
		Annotations map[string]string `json:"annotations"`
		Platform    struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	}
	type index struct {
		Manifests []descriptor `json:"manifests"`
	}
	var top index
	if err := json.Unmarshal(files["index.json"], &top); err != nil {
		t.Fatalf("index.json: %v", err)
	}
	var platforms []string
	named := false
	for _, d := range top.Manifests {
		if strings.HasSuffix(d.Annotations["io.containerd.image.name"], "village-hall:"+version) {
			named = true
		}
		var inner index
		if err := json.Unmarshal(files["blobs/sha256/"+strings.TrimPrefix(d.Digest, "sha256:")], &inner); err != nil {
			t.Fatalf("the index %s: %v", d.Digest, err)
		}
		for _, m := range inner.Manifests {
			platforms = append(platforms, m.Platform.OS+"/"+m.Platform.Architecture)
		}
		if len(inner.Manifests) == 0 && d.Platform.OS != "" {
			platforms = append(platforms, d.Platform.OS+"/"+d.Platform.Architecture)
		}
	}
	sort.Strings(platforms)
	if got := strings.Join(platforms, ","); got != "linux/amd64,linux/arm64" {
		t.Errorf("the OCI index lists %q, want linux/amd64,linux/arm64", got)
	}
	if !named {
		t.Errorf("the OCI index does not name the image village-hall:%s (io.containerd.image.name)", version)
	}
}

// TestReleaseBuildShipsTheInstallerAsCommitted: install.sh and INSTALL-AGENT.md are the commit's.
func TestReleaseBuildShipsTheInstallerAsCommitted(t *testing.T) {
	dir, _ := releaseOut(t)
	for _, name := range []string{"install.sh", "INSTALL-AGENT.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if want := readRepoFile(t, name); string(got) != want {
			t.Errorf("%s in the release differs from the one in the checkout", name)
		}
	}
}
