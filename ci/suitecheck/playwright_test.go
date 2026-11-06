package suitecheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var retriesSetting = regexp.MustCompile(`(?m)^\s*retries\s*:\s*([^,\n]*)`)

// playwrightRetriesProblem returns "" when every `retries` setting in a
// Playwright config is the literal 0 (and there is at least one), otherwise
// what is wrong. Playwright never retries: a flaky spec must fail, and the only
// retry is ci/suite.sh's recorded repeat of a failed run.
func playwrightRetriesProblem(cfg string) string {
	found := retriesSetting.FindAllStringSubmatch(cfg, -1)
	if len(found) == 0 {
		return "no retries setting: it must be spelled out as retries: 0"
	}
	for _, m := range found {
		if v := strings.TrimSpace(m[1]); v != "0" {
			return "retries is " + v + ", not 0"
		}
	}
	return ""
}

// TestPlaywrightConfigNeverRetries is the R11/task 010 probe: the Playwright
// config in e2e/ sets retries: 0, carries the five reporters, and starts the
// real binary (not a mock) on a free port.
func TestPlaywrightConfigNeverRetries(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "e2e", "playwright.config.ts"))
	if err != nil {
		t.Fatalf("read e2e/playwright.config.ts: %v", err)
	}
	cfg := string(raw)
	if problem := playwrightRetriesProblem(cfg); problem != "" {
		t.Errorf("e2e/playwright.config.ts: %s", problem)
	}
	for _, want := range []string{`['html'`, `['list']`, `['allure-playwright'`, `['json'`, `['junit'`, `webServer`, `sh serve.sh`, `listen(0,`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("e2e/playwright.config.ts: missing %q", want)
		}
	}
}

func TestPlaywrightRetriesProblemFlagsAnyRetry(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg    string
		wantOK bool
	}{
		"zero":             {"  retries: 0, // none\n", true},
		"one":              {"  retries: 1,\n", false},
		"ci conditional":   {"  retries: process.env.CI ? 2 : 0,\n", false},
		"absent":           {"  workers: 1,\n", false},
		"second one wrong": {"  retries: 0,\n  retries: 2,\n", false},
	} {
		if got := playwrightRetriesProblem(tc.cfg) == ""; got != tc.wantOK {
			t.Errorf("%s: accepted = %v, want %v", name, got, tc.wantOK)
		}
	}
}

// TestSuiteRunsPlaywrightUnderRetryOnce pins ci/suite.sh's Playwright step:
// the suite-level fail-once-retry-once policy with its flaky and JUnit files.
func TestSuiteRunsPlaywrightUnderRetryOnce(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "ci", "suite.sh"))
	if err != nil {
		t.Fatalf("read ci/suite.sh: %v", err)
	}
	suite := stripCommentLines(string(raw))
	for _, want := range []string{
		`playwright_junit="$outdir/junit-playwright.xml"`,
		`playwright_flaky="$outdir/flaky-playwright.txt"`,
		`E2E_OUT="$outdir/playwright" npx playwright test`,
		`junit-playwright-run1.xml`,
		`playwright: failed once, then passed on the repeat`,
		`Playwright failed twice`,
		`Playwright wrote no JUnit file`,
	} {
		if !strings.Contains(suite, want) {
			t.Errorf("ci/suite.sh: missing %q", want)
		}
	}
	build, pw := strings.Index(suite, "npm run build"), strings.Index(suite, "npx playwright test")
	if build < 0 || pw < build {
		t.Errorf("ci/suite.sh: Playwright must run after the web build (build at %d, Playwright at %d)", build, pw)
	}
}

// specClaimsScenarioPrecision reports whether a document line describes the
// Playwright retry as selecting only failed cases. ci/suite.sh repeats the
// whole `npx playwright test` run, so such a claim is false (R16).
func specClaimsScenarioPrecision(line string) bool {
	l := strings.ToLower(line)
	if !strings.Contains(l, "playwright") {
		return false
	}
	return strings.Contains(l, "scenario-precise") || strings.Contains(l, "only the failed") && !strings.Contains(l, "not only the failed")
}

func TestSpecClaimsScenarioPrecisionFlagsPreciseWording(t *testing.T) {
	for name, tc := range map[string]struct {
		line string
		want bool
	}{
		"deck wording":    {"Playwright (retry-once, scenario-precise like deck's Godog rerun)", true},
		"only failed":     {"Playwright reruns only the failed specs", true},
		"whole run":       {"Playwright: a failed run is repeated whole", false},
		"not only failed": {"Playwright repeats once (every spec runs again, not only the failed ones)", false},
		"godog elsewhere": {"Godog reruns are scenario-precise", false},
	} {
		if got := specClaimsScenarioPrecision(tc.line); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// TestPlaywrightRetryIsDescribedAsAWholeRun: ci/suite.sh repeats the whole
// Playwright run, so SPEC.md and docs/ci.md must not call it scenario-precise,
// and the suite script must not pass a failed-test selection to the repeat.
func TestPlaywrightRetryIsDescribedAsAWholeRun(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	for _, doc := range []string{"SPEC.md", filepath.Join("docs", "ci.md")} {
		raw, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if specClaimsScenarioPrecision(line) {
				t.Errorf("%s:%d claims a scenario-precise Playwright retry the suite does not do", doc, i+1)
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "ci", "suite.sh"))
	if err != nil {
		t.Fatalf("read ci/suite.sh: %v", err)
	}
	for _, flag := range []string{"--last-failed", "--grep"} {
		if strings.Contains(stripCommentLines(string(raw)), flag) {
			t.Errorf("ci/suite.sh passes %s: the repeat is no longer a whole run, update SPEC and docs/ci.md", flag)
		}
	}
}
