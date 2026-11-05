// The SBOM gates (SPEC 15.4): a CycloneDX SBOM generated for the Go
// module, the frontend and the container image, via Trivy. A missing
// input, a generation failure, a missing or empty file, a file that is
// not CycloneDX and one with no component all fail the gate.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sbomOptions describes one SBOM gate run.
type sbomOptions struct {
	Name     string   // report name, e.g. "sbom-module"
	Kind     string   // "fs" (stage Files, scan the staging dir) or "image" (scan the tar at Input)
	Input    string   // the tar for "image"; unused for "fs"
	Files    []string // "fs": the manifests copied into a staging dir (all must exist)
	Binary   string   // trivy executable (default "trivy")
	CacheDir string   // --cache-dir
	OutDir   string   // directory the CycloneDX file is written into
	OutFile  string   // file name inside OutDir
}

// sbomBase carries the flag-supplied options shared by the three gates.
var sbomBase = sbomOptions{OutDir: "ci-results"}

func sbomModule(b sbomOptions) sbomOptions {
	b.Name, b.Kind, b.Files, b.OutFile = "sbom-module", "fs", []string{"go.mod", "go.sum"}, "sbom-module.cdx.json"
	return b
}

func sbomFrontend(b sbomOptions) sbomOptions {
	b.Name, b.Kind, b.Files, b.OutFile = "sbom-frontend", "fs", []string{"web/package.json", "web/package-lock.json"}, "sbom-frontend.cdx.json"
	return b
}

func sbomImage(b sbomOptions) sbomOptions {
	b.Name, b.Kind, b.Input, b.OutFile = "sbom-image", "image", imageTarInput, "sbom-image.cdx.json"
	return b
}

// sbomFail is a gate failure (ok=false, no tooling error) for gate name.
func sbomFail(name, format string, a ...any) (bool, string, error) {
	return false, name + " gate: FAILED, " + fmt.Sprintf(format, a...) + "\n", nil
}

// runSBOMGate generates the SBOM and checks it. ok=false with a nil
// error is a gate failure; an error is a tooling problem (exit 2).
func runSBOMGate(o sbomOptions) (ok bool, output string, err error) {
	out := filepath.Join(o.OutDir, o.OutFile)
	if err := freshOutput(o.OutDir, out); err != nil {
		return false, "", err
	}
	args, cleanup, problem, err := sbomArgs(o, out)
	defer cleanup()
	if err != nil {
		return false, "", err
	}
	if problem != "" {
		return sbomFail(o.Name, "%s", problem)
	}
	bin := o.Binary
	if bin == "" {
		bin = "trivy"
	}
	cmd := exec.Command(bin, args...) //nolint:gosec // G204: the trivy binary is the gate's own option, default trivy
	if text, runErr := cmd.CombinedOutput(); runErr != nil {
		return sbomFail(o.Name, "trivy could not generate the SBOM: %v\n%s", runErr, text)
	}
	n, cerr := checkCycloneDX(out)
	if cerr != nil {
		return sbomFail(o.Name, "%v", cerr)
	}
	return true, fmt.Sprintf("%s gate: ok, %s holds %d components\n", o.Name, out, n), nil
}

// freshOutput creates dir and removes any SBOM an earlier run left at out,
// so a stale file can never satisfy the gate.
func freshOutput(dir, out string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// sbomArgs builds trivy's argv. problem is a gate failure (a missing
// input); cleanup removes any staging dir and is always safe to call.
func sbomArgs(o sbomOptions, out string) (args []string, cleanup func(), problem string, err error) {
	common := []string{"--format", "cyclonedx", "--output", out, "--cache-dir", o.CacheDir, "--skip-version-check", "--quiet"}
	if o.Kind == "image" {
		if _, serr := os.Stat(o.Input); serr != nil {
			return nil, func() {}, "input missing: " + o.Input + " (the image tar the image stage builds)", nil
		}
		return append([]string{"image", "--input", o.Input}, common...), func() {}, "", nil
	}
	if missing := missingFiles(o.Files); len(missing) > 0 {
		return nil, func() {}, "input missing: " + strings.Join(missing, ", "), nil
	}
	stage, err := os.MkdirTemp(o.OutDir, "sbom-stage-")
	if err != nil {
		return nil, func() {}, "", err
	}
	cleanup = func() { _ = os.RemoveAll(stage) }
	if err := stageFiles(o.Files, stage); err != nil {
		return nil, cleanup, "", err
	}
	return append(append([]string{"fs", "--scanners", "license"}, common...), stage), cleanup, "", nil
}

// missingFiles lists the files that do not exist.
func missingFiles(files []string) []string {
	var missing []string
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			missing = append(missing, f)
		}
	}
	return missing
}

// stageFiles copies each manifest into dir, so trivy scans only those.
func stageFiles(files []string, dir string) error {
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // G304: the gate's own fixed manifest list
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.Base(f))
		if err := os.WriteFile(dst, data, 0o600); err != nil { //nolint:gosec // G703: dst is the staging dir plus a fixed manifest's base name
			return err
		}
	}
	return nil
}

// checkCycloneDX returns the component count of the CycloneDX file at
// path, or an error when it is missing, empty, malformed, not CycloneDX
// or lists no component.
func checkCycloneDX(path string) (int, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the gate's own output path
	if err != nil {
		return 0, fmt.Errorf("no SBOM was written at %s", path)
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("the SBOM at %s is empty", path)
	}
	var bom struct {
		BomFormat  string            `json:"bomFormat"`
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(data, &bom); err != nil {
		return 0, fmt.Errorf("the SBOM at %s is not JSON: %w", path, err)
	}
	if bom.BomFormat != "CycloneDX" {
		return 0, fmt.Errorf("the SBOM at %s is not CycloneDX (bomFormat %q)", path, bom.BomFormat)
	}
	if len(bom.Components) == 0 {
		return 0, fmt.Errorf("the SBOM at %s lists no component", path)
	}
	return len(bom.Components), nil
}
