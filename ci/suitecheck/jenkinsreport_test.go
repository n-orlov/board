package suitecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// section is one "=== <name> gate ===" block of a fixture quality report.
func section(name, body, verdict string) string {
	s := "=== " + name + " gate ===\n" + body
	if !strings.HasSuffix(body, "\n") {
		s += "\n"
	}
	if verdict != "" {
		s += "gate verdict: " + verdict + "\n"
	}
	return s
}

// renderSummary runs the real ci/jenkins-report.sh over an output directory
// holding the given quality report (and any extra files, keyed by path under
// that directory), with no suite results, and returns ci-summary/index.html.
// noQualityReport, passed as renderSummary's report, leaves ci-results without
// a quality-report.txt at all (the Quality stage never ran).
const noQualityReport = "\x00no quality report\x00"

func renderSummary(t *testing.T, qualityReport string, extra map[string]string, env ...string) string {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	out := filepath.Join(work, "ci-results")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	if qualityReport != noQualityReport {
		files["quality-report.txt"] = qualityReport
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, content := range files {
		path := filepath.Join(out, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", filepath.Join(root, "ci", "jenkins-report.sh"), out)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "BUILD_URL=", "ALLURE_CACHE_DIR="+filepath.Join(work, "cache"))
	cmd.Env = append(cmd.Env, env...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ci/jenkins-report.sh: %v\n%s", err, combined)
	}
	html, err := os.ReadFile(filepath.Join(work, "ci-summary", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

// badgeOf returns the badge class and text of the report's section for gate.
func badgeOf(t *testing.T, html, gate string) (class, text string) {
	t.Helper()
	m := regexp.MustCompile(`<summary><span class="badge ([a-z]+)">([a-z]+)</span> ` + regexp.QuoteMeta(gate) + ` gate</summary>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no section for gate %s in:\n%s", gate, html)
	}
	return m[1], m[2]
}

// sectionOf returns the whole <details> block of gate.
func sectionOf(t *testing.T, html, gate string) string {
	t.Helper()
	i := strings.Index(html, "</span> "+gate+" gate</summary>")
	if i < 0 {
		t.Fatalf("no section for gate %s", gate)
	}
	start := strings.LastIndex(html[:i], "<details")
	end := strings.Index(html[i:], "</details>")
	return html[start : i+end]
}

func qualityTile(t *testing.T, html string) (class, value string) {
	t.Helper()
	m := regexp.MustCompile(`<div class="tile ([a-z]*)"><div class="v">([a-z-]*)</div><div class="k">quality</div>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no quality tile in:\n%s", html)
	}
	return m[1], m[2]
}

func TestSummaryRendersPassedForEveryPassingGateStyle(t *testing.T) {
	report := section("sbom-image", "sbom-image gate: ok, ... holds 24 components\n", "passed") +
		section("crap-ts", "crap-ts gate: ok\n", "passed") +
		section("crap", "crap gate: ok\nwhat to do: nothing -- every function is at or under the CRAP ceiling.\n", "passed")
	html := renderSummary(t, report, nil)
	for _, gate := range []string{"sbom-image", "crap-ts", "crap"} {
		if class, text := badgeOf(t, html, gate); class != "ok" || text != "passed" {
			t.Errorf("%s: badge %s/%s, want ok/passed", gate, class, text)
		}
		if strings.Contains(sectionOf(t, html, gate), "<details open>") {
			t.Errorf("%s: a passed section must be collapsed", gate)
		}
	}
	if class, value := qualityTile(t, html); class != "ok" || value != "passed" {
		t.Errorf("quality tile %s/%s, want ok/passed", class, value)
	}
}

func TestSummaryRendersFailedAndTurnsTheTileFailed(t *testing.T) {
	report := section("crap", "crap gate: ok\nwhat to do: nothing\n", "passed") +
		section("coverage", "coverage gate: below the floor\nwhat to do: add tests\n", "failed")
	html := renderSummary(t, report, nil)
	if class, text := badgeOf(t, html, "coverage"); class != "bad" || text != "failed" {
		t.Errorf("coverage: badge %s/%s, want bad/failed", class, text)
	}
	if !strings.Contains(sectionOf(t, html, "coverage"), "<details open>") {
		t.Error("a failed section must be open")
	}
	if class, value := qualityTile(t, html); class != "bad" || value != "failed" {
		t.Errorf("quality tile %s/%s, want bad/failed", class, value)
	}
}

// The verdict line decides, never the wording: a body that says "what to do:
// nothing" under a failed verdict is failed, and a body that says it failed
// under a passed verdict is passed.
func TestSummaryNeverReadsAGatesWording(t *testing.T) {
	report := section("crap", "crap gate: ok\nwhat to do: nothing\n", "failed") +
		section("sbom-module", "sbom-module gate: FAILED\nwhat to do: fix it\n", "passed")
	html := renderSummary(t, report, nil)
	if _, text := badgeOf(t, html, "crap"); text != "failed" {
		t.Errorf("crap: %s, want failed despite `what to do: nothing`", text)
	}
	if _, text := badgeOf(t, html, "sbom-module"); text != "passed" {
		t.Errorf("sbom-module: %s, want passed despite a FAILED body", text)
	}
	if _, value := qualityTile(t, html); value != "failed" {
		t.Errorf("tile %s, want failed", value)
	}
}

func TestSummarySectionWithoutAVerdictLineNeverReadsPassed(t *testing.T) {
	report := section("crap", "crap gate: ok\nwhat to do: nothing\n", "")
	html := renderSummary(t, report, nil)
	if _, text := badgeOf(t, html, "crap"); text == "passed" {
		t.Error("a section with no verdict line rendered passed")
	}
	if _, value := qualityTile(t, html); value != "failed" {
		t.Errorf("tile %s, want failed", value)
	}
}

func deferredReport() string {
	return section("crap", "crap gate: ok\n", "passed") +
		section("mutation-go", "mutation-go gate: deferred to the Mutation unit\n", "deferred") +
		section("stryker", "stryker gate: deferred to the Mutation unit\n", "deferred")
}

func TestSummaryDeferredMutationGatesWithoutMutationResults(t *testing.T) {
	html := renderSummary(t, deferredReport(), nil)
	for _, gate := range []string{"mutation-go", "stryker"} {
		class, text := badgeOf(t, html, gate)
		if class != "deferred" || text != "deferred" {
			t.Errorf("%s: badge %s/%s, want deferred/deferred", gate, class, text)
		}
		if !strings.Contains(sectionOf(t, html, gate), "not run in this build") {
			t.Errorf("%s: section lacks \"not run in this build\"", gate)
		}
	}
	if class, value := qualityTile(t, html); class != "ok" || value != "passed" {
		t.Errorf("a report of passed and deferred gates: tile %s/%s, want ok/passed", class, value)
	}
}

func TestSummaryDeferredMutationGatesShowTheMutationUnitsVerdict(t *testing.T) {
	extra := map[string]string{
		"mutation/internal-domain.json":  "{}",
		"mutation/stryker/mutation.json": "{}",
		"mutation/unit-verdict.txt":      "mutation-go: passed\nstryker: failed\n",
	}
	html := renderSummary(t, deferredReport(), extra, "BUILD_URL=https://jenkins.example/job/board/7/")
	goSec, stSec := sectionOf(t, html, "mutation-go"), sectionOf(t, html, "stryker")
	if strings.Contains(goSec, "not run in this build") || strings.Contains(stSec, "not run in this build") {
		t.Error("a build with Mutation results still says not run in this build")
	}
	if !regexp.MustCompile(`Mutation unit verdict: <span class="badge ok">passed</span>`).MatchString(goSec) {
		t.Errorf("mutation-go section lacks the unit's passed verdict:\n%s", goSec)
	}
	if !regexp.MustCompile(`Mutation unit verdict: <span class="badge bad">failed</span>`).MatchString(stSec) {
		t.Errorf("stryker section lacks the unit's failed verdict:\n%s", stSec)
	}
	for _, sec := range []string{goSec, stSec} {
		if !strings.Contains(sec, `href="https://jenkins.example/job/board/7/artifact/ci-results/mutation/"`) {
			t.Errorf("section lacks the link to the Mutation unit's reports:\n%s", sec)
		}
	}
	// The gate's own state in this report is still deferred, and the tile reads this report's gates.
	if _, text := badgeOf(t, html, "mutation-go"); text != "deferred" {
		t.Errorf("badge %s, want deferred", text)
	}
}

func TestSummaryMutationResultsWithoutAVerdictFileLinkToTheReports(t *testing.T) {
	html := renderSummary(t, deferredReport(), map[string]string{"mutation/internal-domain.json": "{}"})
	sec := sectionOf(t, html, "mutation-go")
	if strings.Contains(sec, "not run in this build") || !strings.Contains(sec, `href="ci-results/mutation/"`) {
		t.Errorf("want a link to the reports and no 'not run' claim:\n%s", sec)
	}
}

// Criterion (4): the tile reads passed only when the report holds at least
// one gate and none failed; anything else -- an empty report, a missing one,
// sections with no verdict line -- reads failed, never blank and never passed.
func TestSummaryWithAnEmptyQualityReportReadsFailed(t *testing.T) {
	html := renderSummary(t, "", nil)
	if class, value := qualityTile(t, html); value != "failed" || class != "bad" {
		t.Errorf("tile %q (class %q), want failed (bad)", value, class)
	}
}

func TestSummaryWithNoQualityReportReadsFailed(t *testing.T) {
	html := renderSummary(t, noQualityReport, nil)
	if class, value := qualityTile(t, html); value != "failed" || class != "bad" {
		t.Errorf("tile %q (class %q), want failed (bad)", value, class)
	}
}

func TestSummaryWithSectionsButNoVerdictLinesReadsFailed(t *testing.T) {
	html := renderSummary(t, "=== crap gate ===\ncrap gate: ok\nwhat to do: nothing\n", nil)
	if class, value := qualityTile(t, html); value != "failed" || class != "bad" {
		t.Errorf("tile %q (class %q), want failed (bad)", value, class)
	}
}

func TestJenkinsReportScriptNeverReadsGateWording(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "ci", "jenkins-report.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "what to do") {
		t.Error("ci/jenkins-report.sh mentions `what to do` again: it must read only `gate verdict:` lines")
	}
}
