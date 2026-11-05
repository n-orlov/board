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
