package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mutationReportOK = `{"files":[{"file_name":"a.go","mutations":[{"status":"KILLED"},{"status":"KILLED"},{"status":"KILLED"},{"status":"KILLED"},{"status":"LIVED"}]}]}`
const mutationReportLow = `{"files":[{"file_name":"a.go","mutations":[{"status":"KILLED"},{"status":"LIVED"}]}]}`

func writeMutationReports(t *testing.T, domain, store string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"internal-domain.json": domain, "internal-store.json": store} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMutationGateFailsWithoutReports(t *testing.T) {
	ok, out, err := runMutationGate("ci/quality.json", filepath.Join(t.TempDir(), "absent"))
	if err != nil || ok || !strings.Contains(out, "input missing") {
		t.Fatalf("ok=%v err=%v %s", ok, err, out)
	}
}

func TestMutationGateRunsTheGateToolOverTheReports(t *testing.T) {
	cfg := filepath.Join(repoRoot(t), "ci", "quality.json")
	t.Chdir(repoRoot(t))
	ok, out, err := runMutationGate(cfg, writeMutationReports(t, mutationReportOK, mutationReportOK))
	if err != nil || !ok {
		t.Fatalf("passing reports: ok=%v err=%v %s", ok, err, out)
	}
	ok, out, err = runMutationGate(cfg, writeMutationReports(t, mutationReportOK, mutationReportLow))
	if err != nil || ok || !strings.Contains(out, "below the floor") {
		t.Fatalf("a package at 50%%: ok=%v err=%v %s", ok, err, out)
	}
}
