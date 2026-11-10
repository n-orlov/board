package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mutationReportOK = `{"files":[{"file_name":"a.go","mutations":[{"status":"KILLED"},{"status":"KILLED"},{"status":"KILLED"},{"status":"KILLED"},{"status":"LIVED"}]}]}`
const mutationReportLow = `{"files":[{"file_name":"a.go","mutations":[{"status":"KILLED"},{"status":"LIVED"}]}]}`

// writeMutationReports writes one report per package ci/quality.json
// configures: domain and store get the given content, every other package a
// passing one.
func writeMutationReports(t *testing.T, domain, store string) string {
	t.Helper()
	dir := t.TempDir()
	reports := map[string]string{}
	for _, pkg := range configuredMutationPackages(t) {
		reports[strings.NewReplacer("/", "-", "#", "-").Replace(pkg)+".json"] = mutationReportOK
	}
	reports["internal-domain.json"], reports["internal-store.json"] = domain, store
	for name, content := range reports {
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

// configuredMutationPackages is the units of mutation_go.packages of the real
// ci/quality.json: a package, or "package#part" for each part of a package
// with mutation_go.parts.
func configuredMutationPackages(t *testing.T) []string {
	t.Helper()
	m := realQualityJSON(t)
	sec, ok := m["mutation_go"].(map[string]any)
	if !ok {
		t.Fatal("ci/quality.json has no mutation_go section")
	}
	var pkgs []string
	list, _ := sec["packages"].([]any)
	parts, _ := sec["parts"].(map[string]any)
	for _, p := range list {
		pkg := p.(string)
		split, ok := parts[pkg].(map[string]any)
		if !ok {
			pkgs = append(pkgs, pkg)
			continue
		}
		for name := range split {
			pkgs = append(pkgs, pkg+"#"+name)
		}
	}
	return pkgs
}
