package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodBOM = `{"bomFormat":"CycloneDX","specVersion":"1.6","components":[{"name":"a"},{"name":"b"}]}`

// fakeSBOMTrivy writes a stand-in trivy that copies body to the --output
// path (or writes nothing when body is empty and noOutput is set) and exits 0.
func fakeSBOMTrivy(t *testing.T, body string, noOutput bool, code string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "trivy")
	bodyFile := filepath.Join(dir, "body")
	if err := os.WriteFile(bodyFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nout=\nwhile [ $# -gt 0 ]; do [ \"$1\" = --output ] && out=$2; shift; done\n"
	if !noOutput {
		script += "cat '" + bodyFile + "' > \"$out\"\n"
	}
	script += "exit " + code + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// moduleOpts seeds a working dir with go.mod and go.sum and returns options
// for the module SBOM gate rooted there (the gate reads its manifests
// relative to the working directory).
func moduleOpts(t *testing.T, bin string) sbomOptions {
	t.Helper()
	work := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum"} {
		if err := os.WriteFile(filepath.Join(work, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(work)
	return sbomModule(sbomOptions{Binary: bin, CacheDir: filepath.Join(work, "cache"), OutDir: filepath.Join(work, "out")})
}

func TestSBOMGate(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		noOutput bool
		code     string
		wantOK   bool
		wantText string
	}{
		{"valid CycloneDX passes", goodBOM, false, "0", true, "holds 2 components"},
		{"trivy failing fails", goodBOM, false, "1", false, "could not generate"},
		{"no file written fails", "", true, "0", false, "no SBOM was written"},
		{"empty file fails", "", false, "0", false, "is empty"},
		{"not JSON fails", "<xml/>", false, "0", false, "not JSON"},
		{"wrong format fails", `{"bomFormat":"SPDX","components":[{"name":"a"}]}`, false, "0", false, "not CycloneDX"},
		{"no components fails", `{"bomFormat":"CycloneDX","components":[]}`, false, "0", false, "no component"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := moduleOpts(t, fakeSBOMTrivy(t, tc.body, tc.noOutput, tc.code))
			ok, out, err := runSBOMGate(o)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.wantOK || !strings.Contains(out, tc.wantText) {
				t.Errorf("ok=%v out=%q, want ok=%v containing %q", ok, out, tc.wantOK, tc.wantText)
			}
		})
	}
}

// A stale SBOM from an earlier run never satisfies the gate.
func TestSBOMGateIgnoresAStaleFile(t *testing.T) {
	o := moduleOpts(t, fakeSBOMTrivy(t, "", true, "0"))
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.OutDir, o.OutFile), []byte(goodBOM), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, out, _ := runSBOMGate(o); ok {
		t.Errorf("a stale SBOM passed the gate: %s", out)
	}
}

// The frontend and image gates fail while their inputs do not exist.
func TestSBOMGateMissingInputFails(t *testing.T) {
	bin := fakeSBOMTrivy(t, goodBOM, false, "0")
	t.Chdir(t.TempDir())
	base := sbomOptions{Binary: bin, CacheDir: "cache", OutDir: "out"}
	for name, o := range map[string]sbomOptions{
		"frontend": sbomFrontend(base),
		"image":    sbomImage(base),
		"module":   sbomModule(base),
	} {
		ok, out, err := runSBOMGate(o)
		if err != nil || ok || !strings.Contains(out, "input missing") {
			t.Errorf("%s: ok=%v err=%v out=%q, want a failure naming the missing input", name, ok, err, out)
		}
	}
}

// With the input present the frontend and image gates go through trivy too.
func TestSBOMGateFrontendAndImageGenerate(t *testing.T) {
	bin := fakeSBOMTrivy(t, goodBOM, false, "0")
	work := t.TempDir()
	t.Chdir(work)
	if err := os.MkdirAll("web", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"web/package.json", "web/package-lock.json", imageTarInputName} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := sbomOptions{Binary: bin, CacheDir: "cache", OutDir: "out"}
	for name, o := range map[string]sbomOptions{"frontend": sbomFrontend(base), "image": sbomImage(base)} {
		if ok, out, err := runSBOMGate(o); err != nil || !ok {
			t.Errorf("%s: ok=%v err=%v out=%q", name, ok, err, out)
		}
	}
}

// imageTarInputName is the image tar path, spelled out so a rename of the
// constant is caught here.
const imageTarInputName = "ci-results/board-image.tar"

// A gate whose input does not exist yet fails through run() too: the
// real ci/quality.json, in an empty working tree, fails every gate that
// measures the frontend, the image or the OpenAPI document.
func TestRunFailsEveryGateWhoseInputDoesNotExist(t *testing.T) {
	cfg := filepath.Join(repoRoot(t), "ci", "quality.json")
	bin := fakeSBOMTrivy(t, goodBOM, false, "0")
	t.Chdir(t.TempDir())
	saved := sbomBase
	t.Cleanup(func() { sbomBase = saved })
	sbomBase = sbomOptions{Binary: bin, CacheDir: "cache", OutDir: "out"}
	c, err := loadConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Only the gates that need no tool: everything but deck's five and the module SBOM.
	byName := map[string]bool{}
	for _, g := range newGates(c, cfg) {
		ok, out, rerr := g.run()
		if rerr != nil {
			t.Fatalf("%s: %v", g.name, rerr)
		}
		byName[g.name] = ok
		if g.name == "sbom-module" {
			if ok || !strings.Contains(out, "input missing") {
				t.Errorf("sbom-module in an empty tree: ok=%v %s", ok, out)
			}
			continue
		}
		if ok {
			t.Errorf("gate %s passed with no input: %s", g.name, out)
		}
	}
	for _, want := range []string{"crap-ts", "frontend-coverage", "trivy-image", "mutation-go", "stryker",
		"sbom-module", "sbom-frontend", "sbom-image", "openapi-lint", "openapi-drift"} {
		if _, found := byName[want]; !found {
			t.Errorf("gate %s did not run from the real quality.json", want)
		}
	}
}
