package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenAPIGate_MissingInputFailsAndNamesIt: a tree with no document (or no
// generated code) can never read as clean.
func TestOpenAPIGate_MissingInputFailsAndNamesIt(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, run := range map[string]func() (bool, string, error){
		"lint": runOpenAPILintGate, "drift": runOpenAPIDriftGate,
	} {
		ok, out, err := run()
		if err != nil || ok || !strings.Contains(out, "input missing: api/openapi.yaml") {
			t.Errorf("%s in an empty tree: ok=%v err=%v out=%q", name, ok, err, out)
		}
	}
}

// TestOpenAPIGate_PassesOnExitZeroAndFailsOtherwise drives the shared runner
// with scripts standing in for the real tools.
func TestOpenAPIGate_PassesOnExitZeroAndFailsOtherwise(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "in"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, tc := range []struct {
		script  string
		wantOK  bool
		wantErr bool
	}{
		{"echo fine", true, false},
		{"echo findings; exit 1", false, false},
	} {
		ok, out, err := runOpenAPIGate("g", "fix it", []string{"in"}, openapiCommand{dir: ".", name: "sh", args: []string{"-c", tc.script}})
		if ok != tc.wantOK || (err != nil) != tc.wantErr {
			t.Errorf("%q: ok=%v err=%v out=%q", tc.script, ok, err, out)
		}
		if !tc.wantOK && !strings.Contains(out, "findings") {
			t.Errorf("%q: the tool's own output is missing: %q", tc.script, out)
		}
	}
	_, _, err := runOpenAPIGate("g", "fix it", []string{"in"}, openapiCommand{dir: ".", name: "no-such-tool-board"})
	if err == nil {
		t.Error("a command that cannot start is a tooling error")
	}
}

// TestOpenAPIGates_RunTheScriptsLintRuns pins that the gates run the same
// commands ci/lint.sh does.
func TestOpenAPIGates_RunTheScriptsLintRuns(t *testing.T) {
	lint, err := os.ReadFile(filepath.Join(repoRoot(t), "ci", "lint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"npm run --silent " + strings.Join(openapiLintCommand().args[2:], " "),
		strings.Join(append([]string{openapiDriftCommand().name}, openapiDriftCommand().args...), " "),
	} {
		if !strings.Contains(string(lint), want) {
			t.Errorf("ci/lint.sh does not run %q", want)
		}
	}
}
