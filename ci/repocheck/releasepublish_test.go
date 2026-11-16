package repocheck

// ci/release-publish.sh runs the seven publish steps of SPEC 15.3. These tests
// run the real script with `sh`, with stub gh, docker and curl first on PATH that
// record every call; nothing here reaches GitHub, a registry or Jenkins.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	publishSHA    = "0123456789abcdef0123456789abcdef01234567"
	publishOther  = "fedcba9876543210fedcba9876543210fedcba98"
	publishDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// The stubs answer from STUB_* variables and append one line per call to $STUB_LOG.
var publishStubs = map[string]string{
	"gh": `#!/bin/sh
echo "gh $*" >> "$STUB_LOG"
case "$1 $2" in
"api repos/"*"/git/ref/tags/"*)
	if [ -n "${STUB_TAG_EXISTS-}" ]; then echo '{"ref":"refs/tags/x"}'; exit 0; fi
	echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
"api repos/"*"/releases/tags/"*)
	printf '{"id":1,"tag_name":"x","author":{"login":"a"},"immutable":%s,"assets":[]}\n' "${STUB_IMMUTABLE:-true}"; exit 0 ;;
"release view")
	if [ -n "${STUB_RELEASE_EXISTS-}" ]; then echo "tag_name: x"; exit 0; fi
	echo "release not found" >&2; exit 1 ;;
"release edit")
	while [ $# -gt 0 ]; do
		if [ "$1" = --notes-file ]; then cp "$2" "$STUB_NOTES"; fi
		shift
	done ;;
esac
exit 0
`,
	"curl": `#!/bin/sh
echo "curl $*" >> "$STUB_LOG"
out=""
while [ $# -gt 0 ]; do
	if [ "$1" = -o ]; then out=$2; fi
	shift
done
printf '{"result":"%s","building":false,"actions":[{"_class":"x"},{"lastBuiltRevision":{"SHA1":"%s"}}]}\n' \
	"${STUB_CI_RESULT:-SUCCESS}" "${STUB_CI_SHA}" > "$out"
`,
	"docker": `#!/bin/sh
echo "docker $*" >> "$STUB_LOG"
for arg in "$@"; do
	if [ "$arg" = inspect ]; then echo "$STUB_DIGEST"; fi
done
exit 0
`,
}

// publishRun is one run of the script.
type publishRun struct {
	t      *testing.T
	dir    string
	out    string // the output directory handed to the script
	log    string
	notes  string
	env    []string
	stdout string
	stderr string
	err    error
}

func newPublishRun(t *testing.T, env ...string) *publishRun {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	out := filepath.Join(dir, "out")
	for _, d := range []string{bin, filepath.Join(out, "image"), filepath.Join(dir, "candidate", "ci-results")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range publishStubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"vh_linux_amd64.tar.gz":       "tarball",
		"vh_linux_amd64.cdx.json":     "{}",
		"village-hall-image.cdx.json": "{}",
		"install.sh":                  "#!/bin/sh\n",
		"INSTALL-AGENT.md":            "# agent\n",
		"checksums.txt":               "abc  vh_linux_amd64.tar.gz\n",
		"image/board-image-oci.tar":   "oci",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(out, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidate := `{"commit": "` + publishSHA + `", "build_url": "https://ci.example.com/job/ci/job/main/42/", "build_number": 42}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "candidate", "ci-results", "release-candidate.json"), []byte(candidate), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &publishRun{t: t, dir: dir, out: out, log: filepath.Join(dir, "calls.log"), notes: filepath.Join(dir, "notes.md")}
	r.env = append([]string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"HOME=" + dir,
		"STUB_LOG=" + r.log, "STUB_NOTES=" + r.notes, "STUB_DIGEST=" + publishDigest, "STUB_CI_SHA=" + publishSHA,
		"REPO=example/village-hall", "RELEASE_SHA=" + publishSHA,
		"CANDIDATE=" + filepath.Join(dir, "candidate", "ci-results", "release-candidate.json"),
		"VERSION=v1.2.3", "CI_BUILD=42", "REGISTRY=registry.example.com", "DRY_RUN=false",
	}, env...)
	return r
}

func (r *publishRun) run() *publishRun {
	r.t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		r.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", filepath.Join(root, "ci", "release-publish.sh"), r.out)
	cmd.Dir = r.dir
	cmd.Env = r.env // later entries win for a duplicated name
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r.err = cmd.Run()
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// calls are the recorded stub calls, one per line.
func (r *publishRun) calls() []string {
	raw, err := os.ReadFile(r.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

var writingGH = regexp.MustCompile(`^gh (release (create|upload|edit|delete)|api .*(-X|--method|-f|-F|--field|--raw-field|--input))\b`)

// writing reports whether a recorded call changes anything outside the machine.
func writing(call string) bool {
	switch {
	case strings.HasPrefix(call, "docker "):
		return true // login, run (the push) and anything else on the daemon
	case strings.HasPrefix(call, "gh "):
		return writingGH.MatchString(call)
	case strings.HasPrefix(call, "curl "):
		return regexp.MustCompile(` (-X|--request|-d|--data[a-z-]*|-T|--upload-file|-F|--form) `).MatchString(call + " ")
	}
	return false
}

func (r *publishRun) requireNoWrites() {
	r.t.Helper()
	for _, call := range r.calls() {
		if writing(call) {
			r.t.Errorf("a writing call was made: %q", call)
		}
	}
}

func (r *publishRun) requireRefused(message string) {
	r.t.Helper()
	if r.err == nil {
		r.t.Fatalf("the script succeeded, want a refusal containing %q\nstdout: %s", message, r.stdout)
	}
	if !strings.Contains(r.stderr, message) {
		r.t.Errorf("stderr lacks %q:\n%s", message, r.stderr)
	}
	r.requireNoWrites()
}

func TestPublishDryRunMakesNoWritingCalls(t *testing.T) {
	r := newPublishRun(t, "DRY_RUN=true").run()
	if r.err != nil {
		t.Fatalf("dry run failed: %v\nstdout: %s\nstderr: %s", r.err, r.stdout, r.stderr)
	}
	for i := 1; i <= 7; i++ {
		if want := "step " + string(rune('0'+i)) + "/7"; !strings.Contains(r.stdout, want) {
			t.Errorf("a dry run does not print %q:\n%s", want, r.stdout)
		}
	}
	if len(r.calls()) == 0 {
		t.Error("a dry run read nothing: the candidate and the tag checks did not run")
	}
	r.requireNoWrites()
	if strings.Count(r.stdout, "DRY RUN, would") != 5 {
		t.Errorf("steps 3 to 7 should each say what they would do:\n%s", r.stdout)
	}
}

func TestPublishRealRunFollowsTheSevenSteps(t *testing.T) {
	r := newPublishRun(t).run()
	if r.err != nil {
		t.Fatalf("publish failed: %v\nstdout: %s\nstderr: %s", r.err, r.stdout, r.stderr)
	}
	calls := r.calls()
	at := 0
	for _, want := range []string{
		"curl .*/job/main/42/api/json",                              // 1 the CI build
		"gh api repos/example/village-hall/git/ref/tags/v1.2.3",     // 2 the tag
		"gh release view v1.2.3",                                    // 2 the release
		"gh release create v1.2.3 .*--draft --target " + publishSHA, // 3
		"gh release upload v1.2.3 .*/checksums.txt",                 // 4 every asset
		"gh release upload v1.2.3 .*/install.sh",
		"gh release upload v1.2.3 .*/vh_linux_amd64.tar.gz",
		"gh release upload v1.2.3 .*/village-hall-image.cdx.json",
		"docker run .* copy --all oci-archive:/image/board-image-oci.tar docker://registry.example.com/village-hall:v1.2.3", // 5 push
		"docker run .* inspect --format {{.Digest}} docker://registry.example.com/village-hall:v1.2.3",
		"gh release edit v1.2.3 .*--notes-file",                  // 5 the digest into the notes
		"gh release edit v1.2.3 .*--draft=false",                 // 6 publish
		"gh api repos/example/village-hall/releases/tags/v1.2.3", // 7 read back
	} {
		re := regexp.MustCompile(want)
		found := -1
		for i := at; i < len(calls); i++ {
			if re.MatchString(calls[i]) {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("no call matching %q after call %d, in order; calls:\n%s", want, at, strings.Join(calls, "\n"))
		}
		at = found + 1
	}
	for _, call := range calls {
		if strings.Contains(call, "board-image-oci.tar") && strings.HasPrefix(call, "gh ") {
			t.Errorf("the OCI layout is not an asset: %q", call)
		}
	}
	notes, err := os.ReadFile(r.notes)
	if err != nil || !strings.Contains(string(notes), "registry.example.com/village-hall@"+publishDigest) {
		t.Errorf("the release notes lack the image digest: %v\n%s", err, notes)
	}
}

func TestPublishImmutableFalseExitsNonZeroAfterPublishing(t *testing.T) {
	r := newPublishRun(t, "STUB_IMMUTABLE=false").run()
	if r.err == nil {
		t.Fatalf("a release that is not immutable must fail:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, "not immutable") || !strings.Contains(r.stderr, "immutable-releases") {
		t.Errorf("the failure does not say what to fix:\n%s", r.stderr)
	}
	published := false
	for _, call := range r.calls() {
		published = published || strings.Contains(call, "--draft=false")
	}
	if !published {
		t.Errorf("the draft was never published, so this is not the post-publish failure:\n%s", strings.Join(r.calls(), "\n"))
	}
}

func TestPublishRefusesAnExistingTagBeforeAnyWrite(t *testing.T) {
	for name, env := range map[string]string{"tag": "STUB_TAG_EXISTS=1", "release": "STUB_RELEASE_EXISTS=1"} {
		t.Run(name, func(t *testing.T) {
			r := newPublishRun(t, env).run()
			r.requireRefused("already exists")
		})
	}
}

func TestPublishRefusesACandidateThatIsNotSuccess(t *testing.T) {
	r := newPublishRun(t, "STUB_CI_RESULT=UNSTABLE").run()
	r.requireRefused("not SUCCESS")
}

func TestPublishRefusesACandidateWithADifferingSha(t *testing.T) {
	t.Run("the CI build built another commit", func(t *testing.T) {
		r := newPublishRun(t, "STUB_CI_SHA="+publishOther).run()
		r.requireRefused("did not build the candidate commit")
	})
	t.Run("another commit is checked out", func(t *testing.T) {
		r := newPublishRun(t, "RELEASE_SHA="+publishOther).run()
		r.requireRefused("release exactly the candidate commit")
	})
}

func TestPublishRefusesABadVersion(t *testing.T) {
	for _, version := range []string{"", "1.2.3", "v1.2", "v1.2.3;true", "v1.2.3 ", "latest"} {
		t.Run("version "+version, func(t *testing.T) {
			r := newPublishRun(t, "VERSION="+version).run()
			r.requireRefused("is not vMAJOR.MINOR.PATCH")
			if len(r.calls()) != 0 {
				t.Errorf("a bad VERSION must be refused before any call: %v", r.calls())
			}
		})
	}
}
