// Package lintcheck is the R145 probe for task 013/ci/lint.sh. It EXECUTES
// the script -- a copy of the repository's own ci/lint.sh, run inside a
// throwaway git repository -- with stub `go` and `gofmt` binaries first on
// PATH that record every invocation and can be told to fail or report
// drift. The probe then asserts which stages actually ran, in what order,
// and with what exit status. It therefore fails the moment a stage stops
// being invoked, runs out of order, or a failure/drift no longer stops the
// script -- whatever the script's comments or progress echoes still say.
// Tier 3 probe audit (task 025) added this alongside the ci/suite.sh retry
// probe (and the GitHub Actions-era workflow probes, since removed).
package lintcheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const goStub = `#!/bin/sh
echo "go $*" >> "$LINTPROBE_LOG"
case "$1" in
run)
    if [ -n "${LINTPROBE_INSTALLCHECK_FAIL:-}" ]; then
        echo "stub installcheck: install.sh and INSTALL-AGENT.md disagree" >&2
        exit 1
    fi
    ;;
vet)
    if [ -n "${LINTPROBE_VET_FAIL:-}" ]; then
        echo "stub go vet: failing on request" >&2
        exit 1
    fi
    ;;
mod)
    if [ "${2:-}" = "tidy" ] && [ -n "${LINTPROBE_TIDY_DRIFT:-}" ]; then
        echo "// drift introduced by stub go mod tidy" >> go.mod
    fi
    ;;
esac
exit 0
`

const gofmtStub = `#!/bin/sh
echo "gofmt $*" >> "$LINTPROBE_LOG"
if [ -n "${LINTPROBE_GOFMT_DRIFT:-}" ]; then
    echo "a.go"
fi
exit 0
`

// golangciLintStub records the shared golangci-lint invocation and can be
// told to report findings.
const golangciLintStub = `#!/bin/sh
echo "golangci-lint $*" >> "$LINTPROBE_LOG"
if [ -n "${LINTPROBE_GOLANGCI_FAIL:-}" ]; then
    echo "stub golangci-lint: findings" >&2
    exit 1
fi
exit 0
`

// shellcheckStub records its argument list and can be told to fail.
const shellcheckStub = `#!/bin/sh
echo "shellcheck $*" >> "$LINTPROBE_LOG"
if [ -n "${LINTPROBE_SHELLCHECK_FAIL:-}" ]; then
    echo "stub shellcheck: findings" >&2
    exit 1
fi
exit 0
`

// npmStub records the npm invocation and fails the script named by
// LINTPROBE_NPM_FAIL (lint, check:svelte, check or openapi:lint).
const npmStub = `#!/bin/sh
echo "npm $*" >> "$LINTPROBE_LOG"
for arg in "$@"; do
    if [ -n "${LINTPROBE_NPM_FAIL:-}" ] && [ "$arg" = "$LINTPROBE_NPM_FAIL" ]; then
        echo "stub npm: $arg failing on request" >&2
        exit 1
    fi
done
exit 0
`

// driftStub stands in for ci/openapi-drift.sh, which needs the real Go
// toolchain; the real script is exercised by fixtures_test.go.
const driftStub = `#!/bin/sh
echo "openapi-drift" >> "$LINTPROBE_LOG"
if [ -n "${LINTPROBE_DRIFT_FAIL:-}" ]; then
    echo "stub openapi-drift: stale" >&2
    exit 1
fi
exit 0
`

const originalGoMod = "module example.com/lintprobe\n\ngo 1.25\n"

func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("locate repository root containing go.mod")
		}
		directory = parent
	}
}

type lintRun struct {
	exitCode int
	calls    []string
	output   string
	goMod    string
}

// runLintScript copies the repository's ci/lint.sh into a fresh scratch git
// repository holding one tracked Go file, puts the recording stubs first on
// PATH, runs the script with the given LINTPROBE_* switches, and returns
// its exit code, the ordered stub invocations, its combined output and the
// scratch go.mod as the script left it.
func runLintScript(t *testing.T, switches ...string) lintRun {
	t.Helper()
	return runLintScriptWith(t, scratchOptions{switches: switches})
}

// scratchOptions shapes the scratch repository runLintScriptWith builds.
type scratchOptions struct {
	switches []string
	// realShellcheck leaves the shellcheck stub out so the real binary runs.
	realShellcheck bool
	// extraFiles are written into the scratch repository and tracked.
	extraFiles map[string]string
}

func runLintScriptWith(t *testing.T, opts scratchOptions) lintRun {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(root, "ci", "lint.sh"))
	if err != nil {
		t.Fatalf("read ci/lint.sh: %v", err)
	}
	shared, err := os.ReadFile(filepath.Join(root, "ci", "golangci.sh"))
	if err != nil {
		t.Fatalf("read ci/golangci.sh: %v", err)
	}

	scratch := t.TempDir()
	repo := filepath.Join(scratch, "repo")
	stubs := filepath.Join(scratch, "stubs")
	logPath := filepath.Join(scratch, "calls.log")
	for _, dir := range []string{filepath.Join(repo, "ci"), filepath.Join(repo, "web", "node_modules"), filepath.Join(repo, "api"), stubs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		filepath.Join(repo, "ci", "lint.sh"):          {string(script), 0o755},
		filepath.Join(repo, "ci", "golangci.sh"):      {string(shared), 0o755},
		filepath.Join(repo, "ci", "openapi-drift.sh"): {driftStub, 0o755},
		filepath.Join(repo, "api", "openapi.yaml"):    {"openapi: 3.1.0\n", 0o644},
		filepath.Join(repo, "a.go"):                   {"package lintprobe\n", 0o644},
		filepath.Join(repo, "go.mod"):                 {originalGoMod, 0o644},
		filepath.Join(repo, "go.sum"):                 {"", 0o644},
		filepath.Join(stubs, "go"):                    {goStub, 0o755},
		filepath.Join(stubs, "gofmt"):                 {gofmtStub, 0o755},
		filepath.Join(stubs, "golangci-lint"):         {golangciLintStub, 0o755},
		filepath.Join(stubs, "npm"):                   {npmStub, 0o755},
		logPath:                                       {"", 0o644},
	}
	if !opts.realShellcheck {
		files[filepath.Join(stubs, "shellcheck")] = struct {
			content string
			mode    os.FileMode
		}{shellcheckStub, 0o755}
	}
	tracked := []string{"a.go", "go.mod", "go.sum", "ci/lint.sh", "ci/golangci.sh", "ci/openapi-drift.sh", "api/openapi.yaml"}
	for name, content := range opts.extraFiles {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		files[path] = struct {
			content string
			mode    os.FileMode
		}{content, 0o755}
		tracked = append(tracked, name)
	}
	for path, f := range files {
		if err := os.WriteFile(path, []byte(f.content), f.mode); err != nil {
			t.Fatal(err)
		}
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not on PATH: %v", err)
	}
	for _, args := range [][]string{{"init", "-q"}, append([]string{"add"}, tracked...)} {
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	cmd := exec.Command("sh", filepath.Join(repo, "ci", "lint.sh"))
	cmd.Dir = scratch
	cmd.Env = append(os.Environ(),
		"PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LINTPROBE_LOG="+logPath,
	)
	for _, s := range opts.switches {
		if !strings.Contains(s, "=") {
			s += "=1"
		}
		cmd.Env = append(cmd.Env, s)
	}
	out, runErr := cmd.CombinedOutput()
	result := lintRun{output: string(out)}
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("run ci/lint.sh: %v\n%s", runErr, out)
		}
		result.exitCode = exitErr.ExitCode()
	}
	rawLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(rawLog)), "\n") {
		if line != "" {
			result.calls = append(result.calls, line)
		}
	}
	rawMod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	result.goMod = string(rawMod)
	return result
}

// TestLintScriptRunsAllStagesFailFastAndExitsOnDrift is the probe for
// task 013's ci/lint.sh (golangci-lint added by R188). By executing the
// script against recording stubs it requires that:
//
//   - clean tree: `gofmt -l <tracked .go files>`, then `go vet ./...`, then
//     `go mod tidy`, then `golangci-lint run ./...` (the shared ci/golangci.sh
//     invocation), then shellcheck over the tracked .sh files, ESLint,
//     svelte-check, `tsc --noEmit`, the OpenAPI lint and the generated-code
//     drift check and the install.sh / INSTALL-AGENT.md agreement check are
//     all actually invoked, in that order, and the script
//     exits 0;
//   - a finding from any later stage: the script exits non-zero and no stage
//     after it runs;
//   - golangci-lint findings: the script exits non-zero;
//   - gofmt drift: the script exits non-zero and neither vet nor tidy runs;
//   - vet failure: the script exits non-zero and tidy never runs;
//   - go mod tidy drift: the script exits non-zero and restores go.mod.
//
// It fails when the executable `go vet ./...` line alone is deleted (its
// progress echo left in place), and when any drift branch's `exit 1` is
// deleted.
func TestLintScriptRunsAllStagesFailFastAndExitsOnDrift(t *testing.T) {
	gofmtCall := "gofmt -l a.go"
	vetCall := "go vet ./..."
	tidyCall := "go mod tidy"
	lintCall := "golangci-lint run ./..."
	shellcheckCall := "shellcheck ci/golangci.sh ci/lint.sh ci/openapi-drift.sh"
	eslintCall := "npm run --silent lint"
	svelteCall := "npm run --silent check:svelte"
	tscCall := "npm run --silent check"
	openapiCall := "npm run --silent openapi:lint -- ../api/openapi.yaml"
	driftCall := "openapi-drift"
	installCall := "go run ./ci/installcheck"

	goStages := []string{gofmtCall, vetCall, tidyCall, lintCall}
	upTo := func(calls ...string) []string { return append(append([]string{}, goStages...), calls...) }

	cases := []struct {
		name      string
		switches  []string
		wantFail  bool
		wantCalls []string
	}{
		{"clean tree runs every stage in order and passes", nil, false,
			upTo(shellcheckCall, eslintCall, svelteCall, tscCall, openapiCall, driftCall, installCall)},
		{"gofmt drift fails before vet and tidy", []string{"LINTPROBE_GOFMT_DRIFT"}, true, []string{gofmtCall}},
		{"vet failure fails before tidy", []string{"LINTPROBE_VET_FAIL"}, true, []string{gofmtCall, vetCall}},
		{"go mod tidy drift fails before golangci-lint", []string{"LINTPROBE_TIDY_DRIFT"}, true, []string{gofmtCall, vetCall, tidyCall}},
		{"golangci-lint findings fail before shellcheck", []string{"LINTPROBE_GOLANGCI_FAIL"}, true, goStages},
		{"shellcheck findings fail before ESLint", []string{"LINTPROBE_SHELLCHECK_FAIL"}, true, upTo(shellcheckCall)},
		{"ESLint findings fail before svelte-check", []string{"LINTPROBE_NPM_FAIL=lint"}, true, upTo(shellcheckCall, eslintCall)},
		{"svelte-check findings fail before tsc", []string{"LINTPROBE_NPM_FAIL=check:svelte"}, true, upTo(shellcheckCall, eslintCall, svelteCall)},
		{"tsc findings fail before the OpenAPI lint", []string{"LINTPROBE_NPM_FAIL=check"}, true, upTo(shellcheckCall, eslintCall, svelteCall, tscCall)},
		{"OpenAPI lint findings fail before the drift check", []string{"LINTPROBE_NPM_FAIL=openapi:lint"}, true, upTo(shellcheckCall, eslintCall, svelteCall, tscCall, openapiCall)},
		{"generated-code drift fails before the install agreement check", []string{"LINTPROBE_DRIFT_FAIL"}, true, upTo(shellcheckCall, eslintCall, svelteCall, tscCall, openapiCall, driftCall)},
		{"install.sh and INSTALL-AGENT.md disagreement fails last", []string{"LINTPROBE_INSTALLCHECK_FAIL"}, true, upTo(shellcheckCall, eslintCall, svelteCall, tscCall, openapiCall, driftCall, installCall)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runLintScript(t, tc.switches...)
			if tc.wantFail && got.exitCode == 0 {
				t.Errorf("ci/lint.sh exited 0, want non-zero\noutput:\n%s", got.output)
			}
			if !tc.wantFail && got.exitCode != 0 {
				t.Errorf("ci/lint.sh exited %d, want 0\noutput:\n%s", got.exitCode, got.output)
			}
			if !reflect.DeepEqual(got.calls, tc.wantCalls) {
				t.Errorf("stage invocations = %q, want %q\noutput:\n%s", got.calls, tc.wantCalls, got.output)
			}
			if got.goMod != originalGoMod {
				t.Errorf("ci/lint.sh left go.mod modified:\n%s", got.goMod)
			}
		})
	}
}
