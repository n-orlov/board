// playwrightmode_test.go proves the BOARD_CI_PLAYWRIGHT switch of ci/suite.sh
// (operator ruling 001-WAVE, task 035): the Jenkinsfile runs the Suite unit
// without the Playwright step and the E2E unit with only that step, while a
// plain `ci/suite.sh` still runs everything. It runs the repository's real
// ci/suite.sh against testdata/legfixture/ with stub `npm` and `npx` programs
// on PATH, which record what they were asked to do and write the JUnit files
// the real tools would.
package suitecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stubNpm = `#!/bin/sh
echo "npm $(basename "$PWD") $*" >> "$STUB_LOG"
case "$*" in
*"run test"*)
    mkdir -p "$(dirname "$WEB_JUNIT_FILE")" "$WEB_COVERAGE_DIR"
    echo '<testsuites/>' > "$WEB_JUNIT_FILE"
    ;;
esac
exit 0
`

const stubNpx = `#!/bin/sh
echo "npx $(basename "$PWD") $*" >> "$STUB_LOG"
mkdir -p "$E2E_OUT"
echo '<testsuites/>' > "$E2E_OUT/junit.xml"
exit "${STUB_PLAYWRIGHT_EXIT:-0}"
`

type modeRun struct {
	outdir string
	log    string
	out    []byte
	err    error
}

func runSuiteMode(t *testing.T, mode string, extraEnv ...string) modeRun {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go toolchain not on PATH: %v", err)
	}
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	fixture := filepath.Join(root, "ci", "suitecheck", "testdata", "legfixture")
	module := t.TempDir()
	copyFile(t, filepath.Join(fixture, "go.mod.fixture"), filepath.Join(module, "go.mod"), 0o644)
	for _, f := range []string{"unit.go", "unit_test.go"} {
		copyFile(t, filepath.Join(fixture, "unit", f), filepath.Join(module, "unit", f), 0o644)
	}
	copyFile(t, filepath.Join(fixture, "features", "fixture_test.go"), filepath.Join(module, "features", "fixture_test.go"), 0o644)
	copyFile(t, filepath.Join(root, "ci", "suite.sh"), filepath.Join(module, "ci", "suite.sh"), 0o755)
	for _, tool := range []string{"junitflaky", "junit2allure", "legcompare"} {
		copyFile(t, filepath.Join(root, "ci", tool, "main.go"), filepath.Join(module, "ci", tool, "main.go"), 0o644)
	}
	for _, rel := range []string{"web/package.json", "e2e/package.json"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(module, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(module, rel), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(module, "stubbin")
	for name, body := range map[string]string{"npm": stubNpm, "npx": stubNpx} {
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	run := modeRun{outdir: filepath.Join(module, "out"), log: filepath.Join(module, "stub.log")}
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "BOARD_") || strings.HasPrefix(kv, "GOWORK=") || strings.HasPrefix(kv, "GOCOVERDIR=") ||
			strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GOWORK=off",
		"BOARD_CI_OUT="+run.outdir, "BOARD_CI_GO_PACKAGES=./unit/", "BOARD_CI_DB_LEGS=sqlite",
		"LEGFIXTURE_RED_ON=", "LEGFIXTURE_LEG_LOG="+filepath.Join(module, "legs.txt"), "STUB_LOG="+run.log)
	if mode != "" {
		env = append(env, "BOARD_CI_PLAYWRIGHT="+mode)
	}
	env = append(env, extraEnv...)

	cmd := exec.Command("sh", filepath.Join(module, "ci", "suite.sh"))
	cmd.Dir = module
	cmd.Env = env
	run.out, run.err = cmd.CombinedOutput()
	t.Logf("ci/suite.sh output:\n%s", run.out)
	return run
}

func (r modeRun) has(rel string) bool {
	_, err := os.Stat(filepath.Join(r.outdir, filepath.FromSlash(rel)))
	return err == nil
}

func (r modeRun) stubLog() string {
	raw, _ := os.ReadFile(r.log)
	return string(raw)
}

// A plain ci/suite.sh runs everything, Playwright included.
func TestSuiteWithNoPlaywrightFlagRunsEverythingIncludingPlaywright(t *testing.T) {
	for _, mode := range []string{"", "with"} {
		run := runSuiteMode(t, mode)
		if run.err != nil {
			t.Fatalf("mode %q: ci/suite.sh failed: %v", mode, run.err)
		}
		if !strings.Contains(run.stubLog(), "npx e2e playwright test") {
			t.Errorf("mode %q: Playwright did not run: %q", mode, run.stubLog())
		}
		for _, f := range []string{"junit-playwright.xml", "flaky-playwright.txt", "junit-vitest.xml", "junit-go.xml"} {
			if !run.has(f) {
				t.Errorf("mode %q: %s was not written", mode, f)
			}
		}
	}
}

// Suite's unit: everything but Playwright, and none of Playwright's files.
func TestSuiteSkipsPlaywrightWhenAskedTo(t *testing.T) {
	run := runSuiteMode(t, "skip")
	if run.err != nil {
		t.Fatalf("ci/suite.sh failed: %v", run.err)
	}
	if strings.Contains(run.stubLog(), "playwright") || strings.Contains(run.stubLog(), "npm e2e") {
		t.Errorf("a skipped Playwright step still ran: %q", run.stubLog())
	}
	for _, f := range []string{"junit-playwright.xml", "flaky-playwright.txt", "playwright"} {
		if run.has(f) {
			t.Errorf("skip: %s was written", f)
		}
	}
	for _, f := range []string{"junit-vitest.xml", "flaky-vitest.txt", "junit-go.xml", "flaky-go.txt"} {
		if !run.has(f) {
			t.Errorf("skip: %s was not written (the rest of the suite must run)", f)
		}
	}
}

// E2E's unit: the web build and Playwright only, with its own files.
func TestSuiteRunsOnlyPlaywrightWhenAskedTo(t *testing.T) {
	run := runSuiteMode(t, "only")
	if run.err != nil {
		t.Fatalf("ci/suite.sh failed: %v", run.err)
	}
	log := run.stubLog()
	if !strings.Contains(log, "npm web run build") || !strings.Contains(log, "npx e2e playwright test") {
		t.Errorf("only: the web build and Playwright must both run: %q", log)
	}
	if strings.Contains(log, "run test") {
		t.Errorf("only: Vitest ran: %q", log)
	}
	for _, f := range []string{"junit-playwright.xml", "flaky-playwright.txt"} {
		if !run.has(f) {
			t.Errorf("only: %s was not written", f)
		}
	}
	for _, f := range []string{"junit-vitest.xml", "flaky-vitest.txt", "junit-go.xml", "flaky-go.txt", "junit-merged", "web-build.log"} {
		if run.has(f) {
			t.Errorf("only: %s was written, but the Go and Vitest steps belong to Suite", f)
		}
	}
}

// E2E keeps the one repeat: a run failing twice fails the unit, a pass on the
// repeat is recorded as flaky and exits 0.
func TestSuiteOnlyPlaywrightRepeatsOnceAndFailsOnASecondFailure(t *testing.T) {
	run := runSuiteMode(t, "only", "STUB_PLAYWRIGHT_EXIT=1")
	if run.err == nil {
		t.Fatal("a Playwright run failing twice must fail the E2E unit")
	}
	if got := strings.Count(run.stubLog(), "npx e2e playwright test"); got != 2 {
		t.Errorf("Playwright ran %d times, want the run and one repeat", got)
	}
	if !run.has("junit-playwright-run1.xml") {
		t.Error("the first run's JUnit was not kept as junit-playwright-run1.xml")
	}
}

func TestSuiteRefusesAnUnknownPlaywrightMode(t *testing.T) {
	run := runSuiteMode(t, "sometimes")
	if run.err == nil {
		t.Fatal("ci/suite.sh accepted BOARD_CI_PLAYWRIGHT=sometimes")
	}
	if !strings.Contains(string(run.out), "BOARD_CI_PLAYWRIGHT must be with, skip or only") {
		t.Errorf("the refusal does not say what is allowed:\n%s", run.out)
	}
}
