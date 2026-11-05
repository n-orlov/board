// The SPEC section 15.4 gates beyond deck's five (coverage, CRAP,
// Trivy source, govulncheck, golangci-lint): their thresholds in
// ci/quality.json and the gate runners that are wired into
// ci/qualitycheck.
//
// A gate whose input does not exist yet (the frontend, the image, the
// OpenAPI document) FAILS, never passes: a gate that cannot measure
// anything has measured nothing. The Go mutation gate runs (mutation.go).
// The frontend gates run (frontend.go), and so do the OpenAPI gates
// (openapi.go). The runner for the image Trivy gate lands with the task that
// creates its input; until then pendingGate reports the missing input, or --
// when the input is present but no runner is wired -- says so, and fails in
// both cases.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// crapTSConfig is the TS/Svelte CRAP gate (ESLint complexity x Vitest
// coverage, ceiling 8, no baseline file).
type crapTSConfig struct {
	Enabled bool    `json:"enabled"`
	Ceiling float64 `json:"ceiling"`
}

// frontendCoverageConfig is the frontend coverage gate: statements and
// lines over the whole frontend, and every file individually.
type frontendCoverageConfig struct {
	Enabled    bool    `json:"enabled"`
	TotalFloor float64 `json:"total_floor"`
	FileFloor  float64 `json:"file_floor"`
}

// trivyImageConfig is the Trivy gate over the built container image.
type trivyImageConfig struct {
	Enabled  bool   `json:"enabled"`
	Severity string `json:"severity"`
}

// mutationGoConfig is the gremlins gate: efficacy over the listed
// packages, and a run that generates no mutant fails.
type mutationGoConfig struct {
	Enabled         bool     `json:"enabled"`
	Efficacy        float64  `json:"efficacy"`
	Packages        []string `json:"packages"`
	FailOnNoMutants bool     `json:"fail_on_no_mutants"`
}

// strykerConfig is the Stryker gate over the frontend logic modules:
// the overall break threshold and the per-module one.
type strykerConfig struct {
	Enabled     bool    `json:"enabled"`
	Break       float64 `json:"break"`
	ModuleBreak float64 `json:"module_break"`
	Path        string  `json:"path"`
}

// switchConfig is a gate with no threshold, only an on/off switch.
type switchConfig struct {
	Enabled bool `json:"enabled"`
}

// Inputs the pending gates measure. A gate fails while any is missing.
const (
	imageTarInput = "ci-results/board-image.tar"
	openapiInput  = "api/openapi.yaml"
)

// pendingGate is a gate whose runner has not landed (landsIn names the
// task). It fails when any input is missing, naming each, and fails
// with a "no runner" message when every input is present: it can never
// pass.
func pendingGate(name, landsIn string, inputs ...string) (ok bool, output string, err error) {
	var missing []string
	for _, in := range inputs {
		if _, serr := os.Stat(filepath.Clean(in)); serr != nil {
			missing = append(missing, in)
		}
	}
	if len(missing) > 0 {
		return false, fmt.Sprintf("%s gate: FAILED, input missing: %s\n"+
			"what to do: %s produces it; a gate with nothing to measure fails instead of passing.\n",
			name, strings.Join(missing, ", "), landsIn), nil
	}
	return false, fmt.Sprintf("%s gate: FAILED, its input exists but the gate's runner is not wired yet\n"+
		"what to do: wire the runner (%s); a gate that measures nothing never passes.\n", name, landsIn), nil
}

// looserNew reports the loosening of every gate in this file, relative to base.
func looserNew(base, current config) []string {
	var p []string
	p = append(p, switchedOff("crap_ts", base.CrapTS.Enabled, current.CrapTS.Enabled)...)
	if base.CrapTS.Enabled {
		p = append(p, raisedCeiling("crap_ts.ceiling", base.CrapTS.Ceiling, current.CrapTS.Ceiling)...)
	}
	p = append(p, switchedOff("frontend_coverage", base.FrontendCoverage.Enabled, current.FrontendCoverage.Enabled)...)
	if base.FrontendCoverage.Enabled {
		p = append(p, lowerFloor("frontend_coverage.total_floor", base.FrontendCoverage.TotalFloor, current.FrontendCoverage.TotalFloor)...)
		p = append(p, lowerFloor("frontend_coverage.file_floor", base.FrontendCoverage.FileFloor, current.FrontendCoverage.FileFloor)...)
	}
	p = append(p, prefixAll("image ", looserTrivy(trivyConfig(base.TrivyImage), trivyConfig(current.TrivyImage)))...)
	p = append(p, looserMutation(base.MutationGo, current.MutationGo)...)
	p = append(p, looserStryker(base.Stryker, current.Stryker)...)
	p = append(p, switchedOff("sbom_module", base.SBOMModule.Enabled, current.SBOMModule.Enabled)...)
	p = append(p, switchedOff("sbom_frontend", base.SBOMFrontend.Enabled, current.SBOMFrontend.Enabled)...)
	p = append(p, switchedOff("sbom_image", base.SBOMImage.Enabled, current.SBOMImage.Enabled)...)
	p = append(p, switchedOff("openapi_lint", base.OpenAPILint.Enabled, current.OpenAPILint.Enabled)...)
	p = append(p, switchedOff("openapi_drift", base.OpenAPIDrift.Enabled, current.OpenAPIDrift.Enabled)...)
	return p
}

// looserStryker reports Stryker switched off, a break threshold lowered or
// the measured path changed.
func looserStryker(base, current strykerConfig) []string {
	p := switchedOff("stryker", base.Enabled, current.Enabled)
	if !base.Enabled {
		return p
	}
	p = append(p, lowerFloor("stryker.break", base.Break, current.Break)...)
	p = append(p, lowerFloor("stryker.module_break", base.ModuleBreak, current.ModuleBreak)...)
	if current.Path != base.Path {
		p = append(p, fmt.Sprintf("stryker.path: %q != base %q (the measured modules must not change)", current.Path, base.Path))
	}
	return p
}

func prefixAll(prefix string, in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = prefix + s
	}
	return out
}

// looserMutation reports a mutation gate switched off, an efficacy floor
// lowered, a package dropped, or the zero-mutant failure switched off.
func looserMutation(base, current mutationGoConfig) []string {
	p := switchedOff("mutation_go", base.Enabled, current.Enabled)
	if !base.Enabled {
		return p
	}
	p = append(p, lowerFloor("mutation_go.efficacy", base.Efficacy, current.Efficacy)...)
	have := map[string]bool{}
	for _, pkg := range current.Packages {
		have[pkg] = true
	}
	for _, pkg := range base.Packages {
		if !have[pkg] {
			p = append(p, fmt.Sprintf("mutation_go.packages: %q dropped (base measured it)", pkg))
		}
	}
	if base.FailOnNoMutants && !current.FailOnNoMutants {
		p = append(p, "mutation_go.fail_on_no_mutants: switched off (base had it on)")
	}
	return p
}
