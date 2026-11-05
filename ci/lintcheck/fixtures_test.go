package lintcheck

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture probes for the checkers task 009 added to ci/lint.sh (R14): ESLint,
// svelte-check, tsc, shellcheck, the OpenAPI lint and the generated-code
// drift check. Each test runs the REAL checker, first on an unmodified copy of
// the tree (it must pass, so the failure that follows is the planted
// violation's and nothing else's) and then with one violation planted (it must
// fail and name that violation). ci/lintcheck_test.go proves ci/lint.sh
// invokes every checker, in order, and stops at the first failure.

type commandResult struct {
	exitCode int
	output   string
}

func runCommand(t *testing.T, dir string, env []string, name string, args ...string) commandResult {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	result := commandResult{output: string(out)}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s %v: %v\n%s", name, args, err, out)
		}
		result.exitCode = exitErr.ExitCode()
	}
	return result
}

func requirePasses(t *testing.T, label string, r commandResult) {
	t.Helper()
	if r.exitCode != 0 {
		t.Fatalf("%s: exit %d on the unmodified tree, want 0\n%s", label, r.exitCode, r.output)
	}
}

func requireFailsNaming(t *testing.T, label string, r commandResult, want ...string) {
	t.Helper()
	if r.exitCode == 0 {
		t.Fatalf("%s: exit 0 on the planted violation, want non-zero\n%s", label, r.output)
	}
	for _, w := range want {
		if !strings.Contains(r.output, w) {
			t.Errorf("%s: output does not name %q\n%s", label, w, r.output)
		}
	}
}

// withoutGenerated hides web/'s installed and generated directories from
// os.CopyFS: the copy needs its sources and configuration, nothing else.
type withoutGenerated struct{ fs.FS }

func (w withoutGenerated) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(w.FS, name)
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, e := range entries {
		switch e.Name() {
		case "node_modules", "coverage", "junit", "reports", ".stryker-tmp":
			continue
		}
		kept = append(kept, e)
	}
	return kept, nil
}

// webCopy copies web/ (sources and configuration, not its generated output)
// into a scratch directory next to a copy of api/, links the real
// node_modules in, and returns the scratch web/ directory.
func webCopy(t *testing.T) string {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	modules := filepath.Join(root, "web", "node_modules")
	if _, err := os.Stat(modules); err != nil {
		// A fresh checkout: install from the lockfile, as ci/lint.sh does.
		install := runCommand(t, filepath.Join(root, "web"), nil, "npm", "ci", "--no-audit", "--no-fund")
		requirePasses(t, "npm ci in web/", install)
	}
	scratch := t.TempDir()
	if err := os.CopyFS(filepath.Join(scratch, "web"), withoutGenerated{os.DirFS(filepath.Join(root, "web"))}); err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(scratch, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "api", "openapi.yaml"), spec, 0o644); err != nil { //nolint:gosec // G703: the path is below t.TempDir()
		t.Fatal(err)
	}
	web := filepath.Join(scratch, "web")
	if err := os.Symlink(modules, filepath.Join(web, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return web
}

func plant(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G703: every caller passes a path below t.TempDir()
		t.Fatal(err)
	}
}

func npmScript(t *testing.T, web, script string, extra ...string) commandResult {
	t.Helper()
	args := append([]string{"run", "--silent", script}, extra...)
	return runCommand(t, web, nil, "npm", args...)
}

// TestESLintFailsOnAPlantedViolation: `==` breaks the eqeqeq rule.
func TestESLintFailsOnAPlantedViolation(t *testing.T) {
	web := webCopy(t)
	requirePasses(t, "ESLint", npmScript(t, web, "lint"))
	plant(t, filepath.Join(web, "src", "lib", "planted.ts"),
		"export const same = (a: number, b: number): boolean => a == b;\n")
	requireFailsNaming(t, "ESLint", npmScript(t, web, "lint"), "planted.ts", "eqeqeq")
}

// TestSvelteCheckFailsOnAPlantedViolation: a type error inside a component,
// which tsc --noEmit (it does not read .svelte files) cannot see.
func TestSvelteCheckFailsOnAPlantedViolation(t *testing.T) {
	web := webCopy(t)
	requirePasses(t, "svelte-check", npmScript(t, web, "check:svelte"))
	plant(t, filepath.Join(web, "src", "Planted.svelte"),
		"<script lang=\"ts\">\n  let count: number = 'not a number';\n</script>\n\n<p>{count}</p>\n")
	requireFailsNaming(t, "svelte-check", npmScript(t, web, "check:svelte"), "Planted.svelte", "not assignable to type 'number'")
}

// TestTscFailsOnAPlantedViolation: a type error in a .ts file.
func TestTscFailsOnAPlantedViolation(t *testing.T) {
	web := webCopy(t)
	requirePasses(t, "tsc --noEmit", npmScript(t, web, "check"))
	plant(t, filepath.Join(web, "src", "lib", "planted.ts"), "export const count: number = 'not a number';\n")
	requireFailsNaming(t, "tsc --noEmit", npmScript(t, web, "check"), "planted.ts", "TS2322")
}

// TestOpenAPILintFailsOnAPlantedViolation: an operation with no operationId and
// no summary, in a copy of the real document.
func TestOpenAPILintFailsOnAPlantedViolation(t *testing.T) {
	web := webCopy(t)
	spec := filepath.Join("..", "api", "openapi.yaml")
	requirePasses(t, "OpenAPI lint", npmScript(t, web, "openapi:lint", "--", spec))
	data, err := os.ReadFile(filepath.Join(web, spec))
	if err != nil {
		t.Fatal(err)
	}
	planted := strings.Replace(string(data), "      operationId: getHealthz\n", "", 1)
	if planted == string(data) {
		t.Fatal("api/openapi.yaml no longer carries operationId: getHealthz; update the fixture")
	}
	plant(t, filepath.Join(web, spec), planted)
	requireFailsNaming(t, "OpenAPI lint", npmScript(t, web, "openapi:lint", "--", spec), "operationId")
}

// TestShellcheckFailsOnAPlantedViolation runs the real ci/lint.sh in a scratch
// repository (go, gofmt, golangci-lint and npm stubbed, shellcheck real) with
// one tracked shell script that has an unquoted expansion.
func TestShellcheckFailsOnAPlantedViolation(t *testing.T) {
	clean := runLintScriptWith(t, scratchOptions{realShellcheck: true})
	if clean.exitCode != 0 {
		t.Fatalf("ci/lint.sh exited %d on the unmodified scratch repo, want 0\n%s", clean.exitCode, clean.output)
	}
	planted := runLintScriptWith(t, scratchOptions{
		realShellcheck: true,
		extraFiles:     map[string]string{"planted.sh": "#!/bin/sh\nfor f in $(ls *.txt); do rm -rf $f; done\n"},
	})
	if planted.exitCode == 0 {
		t.Fatalf("ci/lint.sh exited 0 with a planted shellcheck violation\n%s", planted.output)
	}
	if !strings.Contains(planted.output, "planted.sh") || !strings.Contains(planted.output, "SC2") {
		t.Errorf("output does not name the planted script and a shellcheck code\n%s", planted.output)
	}
	for _, call := range planted.calls {
		if strings.HasPrefix(call, "npm ") {
			t.Errorf("a stage after shellcheck ran: %q", call)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestGeneratedCodeDriftFailsOnAPlantedViolation runs the real
// ci/openapi-drift.sh against copies of the checked-in generated files: the
// unmodified copies match, a stale Go file fails, a stale TypeScript file
// fails, a missing file fails, and a document edited without regenerating
// fails.
func TestGeneratedCodeDriftFailsOnAPlantedViolation(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "web", "node_modules")); err != nil {
		install := runCommand(t, filepath.Join(root, "web"), nil, "npm", "ci", "--no-audit", "--no-fund")
		requirePasses(t, "npm ci in web/", install)
	}
	spec := filepath.Join(root, "api", "openapi.yaml")
	goFile := readFile(t, filepath.Join(root, "internal", "server", "openapi.gen.go"))
	tsFile := readFile(t, filepath.Join(root, "web", "src", "lib", "api", "schema.d.ts"))
	specText := readFile(t, spec)

	dir := t.TempDir()
	paths := map[string]string{
		"spec": filepath.Join(dir, "openapi.yaml"),
		"go":   filepath.Join(dir, "openapi.gen.go"),
		"ts":   filepath.Join(dir, "schema.d.ts"),
	}
	reset := func() {
		plant(t, paths["spec"], specText)
		plant(t, paths["go"], goFile)
		plant(t, paths["ts"], tsFile)
	}
	drift := func() commandResult {
		env := []string{"BOARD_OPENAPI_SPEC=" + paths["spec"], "BOARD_OPENAPI_GO=" + paths["go"], "BOARD_OPENAPI_TS=" + paths["ts"]}
		return runCommand(t, root, env, "sh", "ci/openapi-drift.sh")
	}

	reset()
	requirePasses(t, "drift check", drift())

	reset()
	plant(t, paths["go"], goFile+"\n// hand edit\n")
	requireFailsNaming(t, "stale Go file", drift(), paths["go"], "stale")

	reset()
	plant(t, paths["ts"], tsFile+"\n// hand edit\n")
	requireFailsNaming(t, "stale TypeScript file", drift(), paths["ts"], "stale")

	reset()
	if err := os.Remove(paths["go"]); err != nil {
		t.Fatal(err)
	}
	requireFailsNaming(t, "missing Go file", drift(), paths["go"], "missing")

	reset()
	// A new property on Problem changes both generated files.
	edited := strings.Replace(specText, "        instance:\n", "        planted:\n          type: string\n          description: A planted property.\n        instance:\n", 1)
	if edited == specText {
		t.Fatal("api/openapi.yaml no longer carries the Problem.instance property; update the fixture")
	}
	plant(t, paths["spec"], edited)
	requireFailsNaming(t, "document edited without regenerating", drift(), "stale")
}
