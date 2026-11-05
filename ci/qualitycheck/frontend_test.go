package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// summary writes a Vitest json-summary: a total row and one row per file,
// each given as statements and lines percentages.
func summary(total [2]float64, files map[string][2]float64) string {
	row := func(p [2]float64) string {
		return fmt.Sprintf(`{"statements":{"pct":%g},"lines":{"pct":%g}}`, p[0], p[1])
	}
	parts := []string{`"total":` + row(total)}
	for name, p := range files {
		parts = append(parts, fmt.Sprintf("%q:%s", name, row(p)))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func writeSummary(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage-summary.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFrontendCoverageGate(t *testing.T) {
	good := [2]float64{90, 90}
	cases := []struct {
		name    string
		content string
		want    bool
		mention string
	}{
		{"at both floors", summary([2]float64{85, 85}, map[string][2]float64{"/w/web/src/lib/a.ts": {80, 80}}), true, "ok"},
		{"total statements below 85", summary([2]float64{84.99, 90}, map[string][2]float64{"a.ts": good}), false, "total: statements 84.99%"},
		{"total lines below 85", summary([2]float64{90, 84}, map[string][2]float64{"a.ts": good}), false, "total: lines 84.00%"},
		{"one file statements below 80", summary(good, map[string][2]float64{"a.ts": good, "b.ts": {79.9, 100}}), false, "b.ts: statements 79.90%"},
		{"one file lines below 80", summary(good, map[string][2]float64{"a.ts": good, "b.ts": {100, 10}}), false, "b.ts: lines 10.00%"},
		{"no file listed", summary(good, nil), false, "lists no file"},
		{"a file with nothing to measure is skipped", `{"total":{"statements":{"pct":90},"lines":{"pct":90}},"t.ts":{"statements":{"pct":"Unknown"},"lines":{"pct":"Unknown"}},"a.ts":{"statements":{"pct":90},"lines":{"pct":90}}}`, true, "ok"},
		{"a total with no percentage", `{"total":{"statements":{"pct":"Unknown"},"lines":{"pct":"Unknown"}},"a.ts":{"statements":{"pct":90},"lines":{"pct":90}}}`, false, "total statements has no percentage"},
		{"no total row", `{"a.ts":{"statements":{"pct":90},"lines":{"pct":90}}}`, false, "has no total row"},
		{"not JSON", `nope`, false, "not a coverage summary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, out, err := runFrontendCoverageGate(writeSummary(t, c.content), 85, 80)
			if err != nil || ok != c.want || !strings.Contains(out, c.mention) {
				t.Errorf("ok=%v err=%v out=%q, want ok=%v mentioning %q", ok, err, out, c.want, c.mention)
			}
		})
	}
}

func TestFrontendCoverageGateMissingSummaryFails(t *testing.T) {
	ok, out, err := runFrontendCoverageGate(filepath.Join(t.TempDir(), "none.json"), 85, 80)
	if err != nil || ok || !strings.Contains(out, "input missing") {
		t.Errorf("ok=%v err=%v out=%q", ok, err, out)
	}
}

// ci/quality.sh itself, with only the frontend coverage gate switched on, runs
// the gate code on a planted coverage summary: exit 1 below a floor, 0 above.
func TestQualityShFrontendCoverageGate(t *testing.T) {
	root := repoRoot(t)
	cfg := writeTempFile(t, `{"frontend_coverage":{"enabled":true,"total_floor":85,"file_floor":80}}`)
	run := func(content string) (string, int) {
		outdir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(outdir, "web-coverage"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outdir, "web-coverage", "coverage-summary.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", "ci/quality.sh", outdir, cfg)
		cmd.Dir = root
		cmd.Env = goChildEnv()
		out, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return string(out), exitErr.ExitCode()
		}
		if err != nil {
			t.Fatalf("ci/quality.sh did not run: %v\n%s", err, out)
		}
		return string(out), 0
	}
	if out, code := run(summary([2]float64{90, 90}, map[string][2]float64{"a.ts": {90, 90}})); code != 0 || !strings.Contains(out, "=== frontend-coverage gate ===") {
		t.Errorf("a covered frontend: exit %d\n%s", code, out)
	}
	if out, code := run(summary([2]float64{84, 90}, map[string][2]float64{"a.ts": {90, 90}})); code != 1 || !strings.Contains(out, "below the floor") {
		t.Errorf("total below the floor: exit %d\n%s", code, out)
	}
	if out, code := run(summary([2]float64{90, 90}, map[string][2]float64{"a.ts": {90, 90}, "b.ts": {50, 90}})); code != 1 || !strings.Contains(out, "b.ts: statements 50.00%") {
		t.Errorf("one file below the floor: exit %d\n%s", code, out)
	}
}

// fakeNode puts a stand-in "node" in a temp web/ project: it records its argv
// and working directory and exits 0.
func fakeNode(t *testing.T) (argvLog string) {
	t.Helper()
	work := t.TempDir()
	argvLog = filepath.Join(work, "argv.log")
	bin := filepath.Join(work, "node")
	script := fmt.Sprintf("#!/bin/sh\necho \"$PWD $*\" > %q\necho script output\nexit 0\n", argvLog)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := nodeBin
	t.Cleanup(func() { nodeBin = saved })
	nodeBin = bin
	t.Chdir(work)
	for _, d := range []string{"web/node_modules/eslint", "web/node_modules/typescript", "out/web-coverage", "out/mutation/stryker"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"out/web-coverage/coverage-final.json", "out/mutation/stryker/mutation.json"} {
		if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return argvLog
}

func TestCrapTSGateRunsTheScriptFromWebAndFailsWhenItFails(t *testing.T) {
	argvLog := fakeNode(t)
	ok, out, err := runCrapTSGate("ci/quality.json", "out")
	if err != nil || !ok || !strings.Contains(out, "script output") {
		t.Fatalf("passing script: ok=%v err=%v out=%q", ok, err, out)
	}
	logged, _ := os.ReadFile(argvLog)
	for _, want := range []string{"/web scripts/crap/crap.ts", "--coverage ", "out/web-coverage/coverage-final.json", "--config ", "ci/quality.json"} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("argv %q lacks %q", logged, want)
		}
	}
	if strings.Contains(string(logged), "baseline") {
		t.Errorf("a baseline reached the script: %q", logged)
	}
	nodeBin = writeFailingNode(t)
	ok, out, err = runCrapTSGate("ci/quality.json", "out")
	if err != nil || ok || !strings.Contains(out, "what to do") || !strings.Contains(out, "never raise crap_ts.ceiling") {
		t.Errorf("failing script: ok=%v err=%v out=%q", ok, err, out)
	}
}

func writeFailingNode(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho CRAP 56 over\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestCrapTSAndStrykerGatesFailWithoutTheirInputs(t *testing.T) {
	fakeNode(t)
	for name, remove := range map[string]string{
		"crap-ts coverage":   "out/web-coverage/coverage-final.json",
		"crap-ts eslint":     "web/node_modules/eslint",
		"stryker report":     "out/mutation/stryker/mutation.json",
		"stryker typescript": "web/node_modules/typescript",
	} {
		t.Run(name, func(t *testing.T) {
			fakeNode(t)
			if err := os.RemoveAll(remove); err != nil {
				t.Fatal(err)
			}
			gate := runCrapTSGate
			if strings.HasPrefix(name, "stryker") {
				gate = runStrykerGate
			}
			ok, out, err := gate("ci/quality.json", "out")
			if err != nil || ok || !strings.Contains(out, "input missing: "+remove) {
				t.Errorf("ok=%v err=%v out=%q", ok, err, out)
			}
		})
	}
}

func TestStrykerGateJudgesTheCopiedReportAndFailsWhenTheScriptFails(t *testing.T) {
	argvLog := fakeNode(t)
	if ok, out, err := runStrykerGate("ci/quality.json", "out"); err != nil || !ok {
		t.Fatalf("passing script: ok=%v err=%v out=%q", ok, err, out)
	}
	logged, _ := os.ReadFile(argvLog)
	for _, want := range []string{"scripts/mutation/mutation.ts", "--check-only", "out/mutation/stryker/mutation.json"} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("argv %q lacks %q", logged, want)
		}
	}
	nodeBin = writeFailingNode(t)
	if ok, out, err := runStrykerGate("ci/quality.json", "out"); err != nil || ok || !strings.Contains(out, "never lower stryker.break") {
		t.Errorf("failing script: ok=%v err=%v out=%q", ok, err, out)
	}
}

// The frontend SBOM gate: trivy runs over web's manifests with the dev
// dependencies included (all of the frontend's are), and a missing, empty or
// component-less SBOM fails it.
func TestSBOMFrontendGate(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		noOutput bool
		wantOK   bool
		wantText string
	}{
		{"components pass", goodBOM, false, true, "holds 2 components"},
		{"missing file fails", "", true, false, "no SBOM was written"},
		{"empty file fails", "", false, false, "is empty"},
		{"no component fails", `{"bomFormat":"CycloneDX","components":[]}`, false, false, "no component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)
			if err := os.MkdirAll("web", 0o755); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"web/package.json", "web/package-lock.json"} {
				if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			o := sbomFrontend(sbomOptions{Binary: fakeSBOMTrivy(t, tc.body, tc.noOutput, "0"), CacheDir: "cache", OutDir: "out"})
			ok, out, err := runSBOMGate(o)
			if err != nil || ok != tc.wantOK || !strings.Contains(out, tc.wantText) {
				t.Errorf("ok=%v err=%v out=%q, want ok=%v containing %q", ok, err, out, tc.wantOK, tc.wantText)
			}
		})
	}
}

func TestSBOMFrontendIncludesDevDependencies(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("web", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"web/package.json", "web/package-lock.json"} {
		if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args, cleanup, problem, err := sbomArgs(sbomFrontend(sbomOptions{OutDir: "."}), "out.json")
	defer cleanup()
	if err != nil || problem != "" || !strings.Contains(strings.Join(args, " "), "--include-dev-deps") {
		t.Errorf("args=%v problem=%q err=%v, want --include-dev-deps", args, problem, err)
	}
}
