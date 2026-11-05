// The frontend gates (SPEC 15.4): TS CRAP, frontend coverage and Stryker.
// Each reads what ci/suite.sh (web-coverage/) and ci/mutation.sh
// (mutation/stryker/) wrote under the suite output directory, so a run that
// produced nothing measured nothing and fails.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// webDir is the frontend project; the Node tools run from it.
const webDir = "web"

// nodeBin is the Node executable the TS gates run (a var so a test can point it at a stand-in).
var nodeBin = "node"

// webCoverageDir is where ci/suite.sh puts Vitest's coverage reports.
func webCoverageDir(outdir string) string { return filepath.Join(outdir, "web-coverage") }

// strykerReport is the Stryker JSON report ci/mutation.sh copies out of web/.
func strykerReport(outdir string) string {
	return filepath.Join(mutationReportDir(outdir), "stryker", "mutation.json")
}

// missingInputs lists the paths that do not exist.
func missingInputs(paths ...string) []string {
	var missing []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, p)
		}
	}
	return missing
}

// inputMissing is the failure for a gate whose input is not there.
func inputMissing(name, producer string, missing []string) (bool, string, error) {
	return false, fmt.Sprintf("%s gate: FAILED, input missing: %s\n"+
		"what to do: %s produces it; a gate with nothing to measure fails instead of passing.\n",
		name, strings.Join(missing, ", "), producer), nil
}

// runNodeGate runs `node <script> <args...>` from web/ and passes on exit 0.
// The script's own output is the report; what to do is appended on failure.
func runNodeGate(name, script, advice string, args ...string) (bool, string, error) {
	cmd := exec.Command(nodeBin, append([]string{script}, args...)...) //nolint:gosec // G204: node runs an in-repo script with the gate's own fixed arguments
	cmd.Dir = webDir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		return true, name + " gate: ok\n" + text, nil
	}
	return false, name + " gate: FAILED (" + err.Error() + ")\n" + text + "what to do: " + advice + "\n", nil
}

// runCrapTSGate scores web/src with scripts/crap (ESLint complexity x the
// Vitest coverage in outdir). The ceiling is read by the script from
// configPath's crap_ts.ceiling; there is no baseline.
func runCrapTSGate(configPath, outdir string) (bool, string, error) {
	coverage := filepath.Join(webCoverageDir(outdir), "coverage-final.json")
	if missing := missingInputs(coverage, filepath.Join(webDir, "node_modules", "eslint")); len(missing) > 0 {
		return inputMissing("crap-ts", "ci/suite.sh (npm ci, then Vitest with coverage)", missing)
	}
	absCoverage, aerr := filepath.Abs(coverage)
	absConfig, cerr := filepath.Abs(configPath)
	if aerr != nil || cerr != nil {
		return false, "", fmt.Errorf("resolving the crap-ts inputs: %w", errors.Join(aerr, cerr))
	}
	return runNodeGate("crap-ts", "scripts/crap/crap.ts",
		"split or simplify the named function(s), or add tests for them; never raise crap_ts.ceiling or add a baseline.",
		"--coverage", absCoverage, "--config", absConfig)
}

// runStrykerGate judges the Stryker report ci/mutation.sh wrote with
// scripts/mutation (whole-run break and per-module break from configPath).
func runStrykerGate(configPath, outdir string) (bool, string, error) {
	report := strykerReport(outdir)
	if missing := missingInputs(report, filepath.Join(webDir, "node_modules", "typescript")); len(missing) > 0 {
		return inputMissing("stryker", "ci/mutation.sh", missing)
	}
	absReport, aerr := filepath.Abs(report)
	absConfig, cerr := filepath.Abs(configPath)
	if aerr != nil || cerr != nil {
		return false, "", fmt.Errorf("resolving the stryker inputs: %w", errors.Join(aerr, cerr))
	}
	return runNodeGate("stryker", "scripts/mutation/mutation.ts",
		"add tests that kill the surviving mutants of the named module(s); never lower stryker.break or stryker.module_break.",
		"--check-only", "--report", absReport, "--config", absConfig)
}

// coverageMetric is one Istanbul summary metric; pct is a number, or the
// string "Unknown" for a file with nothing to measure.
type coverageMetric struct {
	Pct any `json:"pct"`
}

// coverageEntry is one row of coverage-summary.json (the "total" row or a file).
type coverageEntry struct {
	Statements coverageMetric `json:"statements"`
	Lines      coverageMetric `json:"lines"`
}

// percent returns the metric's percentage, false when it has none.
func (m coverageMetric) percent() (float64, bool) {
	f, ok := m.Pct.(float64)
	return f, ok && !math.IsNaN(f)
}

// runFrontendCoverageGate checks the Vitest json-summary at summaryPath:
// statements and lines of the whole frontend must reach totalFloor and of
// every file fileFloor.
func runFrontendCoverageGate(summaryPath string, totalFloor, fileFloor float64) (bool, string, error) {
	data, err := os.ReadFile(summaryPath) //nolint:gosec // G304: the gate's own input path
	if err != nil {
		return inputMissing("frontend-coverage", "ci/suite.sh (Vitest with coverage)", []string{summaryPath})
	}
	var rows map[string]coverageEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return false, fmt.Sprintf("frontend-coverage gate: FAILED, %s is not a coverage summary: %v\n", summaryPath, err), nil
	}
	var problems []string
	total, hasTotal := rows["total"]
	if !hasTotal {
		problems = append(problems, summaryPath+" has no total row")
	}
	problems = append(problems, belowFloor("total", total, totalFloor)...)
	files := make([]string, 0, len(rows))
	for name := range rows {
		if name != "total" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		problems = append(problems, "the summary lists no file: nothing was measured")
	}
	for _, name := range files {
		problems = append(problems, belowFloor(name, rows[name], fileFloor)...)
	}
	if len(problems) > 0 {
		return false, "frontend-coverage gate: FAILED\n  " + strings.Join(problems, "\n  ") + "\n" +
			"what to do: add tests for the named file(s); never lower a floor in ci/quality.json or exclude a measured file.\n", nil
	}
	return true, fmt.Sprintf("frontend-coverage gate: ok, %d files, statements %.2f%% lines %.2f%% (floors %g total, %g per file)\n",
		len(files), pctOf(total.Statements), pctOf(total.Lines), totalFloor, fileFloor), nil
}

func pctOf(m coverageMetric) float64 {
	f, _ := m.percent()
	return f
}

// belowFloor reports each of the row's statements and lines percentages
// that is under floor. A file with no percentage has nothing to measure
// and is skipped; the total row must have one.
func belowFloor(name string, e coverageEntry, floor float64) []string {
	var p []string
	for _, m := range []struct {
		label  string
		metric coverageMetric
	}{{"statements", e.Statements}, {"lines", e.Lines}} {
		pct, ok := m.metric.percent()
		switch {
		case !ok && name == "total":
			p = append(p, "total "+m.label+" has no percentage")
		case ok && pct < floor:
			p = append(p, fmt.Sprintf("%s: %s %.2f%% is below the floor %g%%", name, m.label, pct, floor))
		}
	}
	return p
}
