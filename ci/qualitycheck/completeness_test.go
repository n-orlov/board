package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// SPEC 15.4 and R13 name every gate ci/quality.json must carry, enabled,
// at its threshold. specGaps is that list written down independently of
// the checked-in file: TestEverySpecGateIsInQualityJSON holds the real
// file to it, and the table test below proves it notices a gate that is
// removed, switched off, or loosened past its threshold.

func atLeast(gaps *[]string, name string, got, want float64) {
	if got < want {
		*gaps = append(*gaps, fmt.Sprintf("%s: %v is below the required %v", name, got, want))
	}
}

func atMost(gaps *[]string, name string, got, want float64) {
	if got > want {
		*gaps = append(*gaps, fmt.Sprintf("%s: %v is above the allowed %v", name, got, want))
	}
}

func covers(gaps *[]string, name, have string, want ...string) {
	set := severitySet(have)
	for _, w := range want {
		if !set[w] {
			*gaps = append(*gaps, fmt.Sprintf("%s: %q does not cover %s", name, have, w))
		}
	}
}

func enabled(gaps *[]string, name string, on bool) bool {
	if !on {
		*gaps = append(*gaps, name+": missing or enabled is not true")
	}
	return on
}

// specGaps returns one line per way cfg falls short of SPEC 15.4; each
// line starts with the gate's key in ci/quality.json.
func specGaps(cfg config) []string {
	var g []string
	if enabled(&g, "coverage", cfg.Coverage.Enabled) {
		atLeast(&g, "coverage.total_floor", cfg.Coverage.TotalFloor, 85)
		atLeast(&g, "coverage.package_floor", cfg.Coverage.PackageFloor, 80)
		atLeast(&g, "coverage.fixture_floor", cfg.Coverage.FixtureFloor, 50)
	}
	if enabled(&g, "crap", cfg.Crap.Enabled) {
		if cfg.Crap.Ceiling <= 0 || cfg.Crap.FixtureCeiling <= 0 {
			g = append(g, "crap: a ceiling is missing or zero")
		}
		atMost(&g, "crap.ceiling", cfg.Crap.Ceiling, 8)
		atMost(&g, "crap.fixture_ceiling", cfg.Crap.FixtureCeiling, 8)
	}
	if enabled(&g, "crap_ts", cfg.CrapTS.Enabled) {
		if cfg.CrapTS.Ceiling <= 0 {
			g = append(g, "crap_ts: the ceiling is missing or zero")
		}
		atMost(&g, "crap_ts.ceiling", cfg.CrapTS.Ceiling, 8)
	}
	if enabled(&g, "frontend_coverage", cfg.FrontendCoverage.Enabled) {
		atLeast(&g, "frontend_coverage.total_floor", cfg.FrontendCoverage.TotalFloor, 85)
		atLeast(&g, "frontend_coverage.file_floor", cfg.FrontendCoverage.FileFloor, 80)
	}
	enabled(&g, "golangci", cfg.Golangci.Enabled)
	enabled(&g, "govulncheck", cfg.Govulncheck.Enabled)
	if enabled(&g, "trivy", cfg.Trivy.Enabled) {
		covers(&g, "trivy.severity", cfg.Trivy.Severity, "HIGH", "CRITICAL")
	}
	if enabled(&g, "trivy_image", cfg.TrivyImage.Enabled) {
		covers(&g, "trivy_image.severity", cfg.TrivyImage.Severity, "HIGH", "CRITICAL")
	}
	if enabled(&g, "mutation_go", cfg.MutationGo.Enabled) {
		atLeast(&g, "mutation_go.efficacy", cfg.MutationGo.Efficacy, 80)
		have := map[string]bool{}
		for _, p := range cfg.MutationGo.Packages {
			have[p] = true
		}
		// Phase 1's packages, then the handwritten Go application logic
		// Phase 2 introduced or changed.
		for _, p := range []string{"internal/domain", "internal/store", "internal/asks", "internal/attention",
			"internal/checkin", "internal/cursors", "internal/events", "internal/feed", "internal/notes",
			"internal/server", "internal/cli"} {
			if !have[p] {
				g = append(g, "mutation_go.packages: "+p+" is not measured")
			}
		}
		for p, files := range cfg.MutationGo.Scopes {
			if !have[p] || len(files) == 0 {
				g = append(g, "mutation_go.scopes: "+p+" is scoped to nothing or is not a measured package")
			}
		}
		for _, f := range []string{"askops.go", "askwait.go", "attentionops.go", "cursorops.go", "noteops.go", "streamops.go"} {
			if scope, ok := cfg.MutationGo.Scopes["internal/server"]; ok && !slices.Contains(scope, f) {
				g = append(g, "mutation_go.scopes: internal/server lacks "+f)
			}
		}
		for _, f := range []string{"askpoll.go", "asks.go", "attention.go", "notes.go"} {
			if scope, ok := cfg.MutationGo.Scopes["internal/cli"]; ok && !slices.Contains(scope, f) {
				g = append(g, "mutation_go.scopes: internal/cli lacks "+f)
			}
		}
		if !cfg.MutationGo.FailOnNoMutants {
			g = append(g, "mutation_go.fail_on_no_mutants: a run with no mutant must fail")
		}
	}
	if enabled(&g, "stryker", cfg.Stryker.Enabled) {
		atLeast(&g, "stryker.break", cfg.Stryker.Break, 80)
		atLeast(&g, "stryker.module_break", cfg.Stryker.ModuleBreak, 80)
		if cfg.Stryker.Path != "web/src/lib" {
			g = append(g, fmt.Sprintf("stryker.path: %q, want web/src/lib", cfg.Stryker.Path))
		}
	}
	enabled(&g, "sbom_module", cfg.SBOMModule.Enabled)
	enabled(&g, "sbom_frontend", cfg.SBOMFrontend.Enabled)
	enabled(&g, "sbom_image", cfg.SBOMImage.Enabled)
	enabled(&g, "openapi_lint", cfg.OpenAPILint.Enabled)
	enabled(&g, "openapi_drift", cfg.OpenAPIDrift.Enabled)
	return g
}

// decodeStrict parses raw as a config, refusing keys the config does not
// know (a misspelt threshold would otherwise read as zero or off).
func decodeStrict(raw []byte) (config, error) {
	var cfg config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&cfg)
	return cfg, err
}

func realQualityJSON(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "ci", "quality.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeStrict(raw); err != nil {
		t.Fatalf("ci/quality.json has a key the config does not know: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEverySpecGateIsInQualityJSON(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(repoRoot(t), "ci", "quality.json"))
	if err != nil {
		t.Fatal(err)
	}
	if gaps := specGaps(cfg); len(gaps) != 0 {
		t.Errorf("ci/quality.json falls short of SPEC 15.4:\n%s", strings.Join(gaps, "\n"))
	}
	realQualityJSON(t) // strict decode
}

type loosening struct {
	field string // key inside the gate's section
	value any    // a value past the threshold
}

// TestSpecGapsCatchesARemovedDisabledOrLoosenedGate removes, disables and
// loosens each gate in a copy of the real file and requires specGaps to
// name that gate; the unmodified copy must have no gap.
func TestSpecGapsCatchesARemovedDisabledOrLoosenedGate(t *testing.T) {
	gates := []struct {
		key      string
		loosened []loosening
	}{
		{"coverage", []loosening{{"total_floor", 84.0}, {"package_floor", 79.0}, {"fixture_floor", 49.0}}},
		{"crap", []loosening{{"ceiling", 9.0}, {"fixture_ceiling", 9.0}, {"ceiling", 0.0}}},
		{"crap_ts", []loosening{{"ceiling", 9.0}, {"ceiling", 0.0}}},
		{"frontend_coverage", []loosening{{"total_floor", 84.0}, {"file_floor", 79.0}}},
		{"golangci", nil},
		{"govulncheck", nil},
		{"trivy", []loosening{{"severity", "CRITICAL"}, {"severity", "HIGH"}, {"severity", ""}}},
		{"trivy_image", []loosening{{"severity", "CRITICAL"}, {"severity", "HIGH"}, {"severity", ""}}},
		{"mutation_go", []loosening{
			{"efficacy", 79.0}, {"packages", []any{"internal/domain"}}, {"packages", []any{"internal/store"}},
			{"packages", []any{}}, {"fail_on_no_mutants", false},
			{"packages", []any{"internal/domain", "internal/store"}},
			{"scopes", map[string]any{"internal/server": []any{"askops.go"}}},
			{"scopes", map[string]any{"internal/cli": []any{"asks.go"}}},
			{"scopes", map[string]any{"internal/audit": []any{}}}}},
		{"stryker", []loosening{{"break", 79.0}, {"module_break", 79.0}, {"path", "web/src"}}},
		{"sbom_module", nil},
		{"sbom_frontend", nil},
		{"sbom_image", nil},
		{"openapi_lint", nil},
		{"openapi_drift", nil},
	}

	mutate := func(t *testing.T, edit func(m map[string]any)) []string {
		m := realQualityJSON(t)
		edit(m)
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := decodeStrict(raw)
		if err != nil {
			t.Fatal(err)
		}
		return specGaps(cfg)
	}
	section := func(m map[string]any, key string) map[string]any {
		s, ok := m[key].(map[string]any)
		if !ok {
			t.Fatalf("ci/quality.json has no %q section", key)
		}
		return s
	}

	if gaps := mutate(t, func(map[string]any) {}); len(gaps) != 0 {
		t.Fatalf("the unmodified real file has gaps: %v", gaps)
	}
	for _, g := range gates {
		wantIn := func(t *testing.T, gaps []string) {
			t.Helper()
			for _, line := range gaps {
				if strings.HasPrefix(line, g.key) {
					return
				}
			}
			t.Errorf("no gap names %q: %v", g.key, gaps)
		}
		t.Run(g.key+"/removed", func(t *testing.T) {
			wantIn(t, mutate(t, func(m map[string]any) { section(m, g.key); delete(m, g.key) }))
		})
		t.Run(g.key+"/disabled", func(t *testing.T) {
			wantIn(t, mutate(t, func(m map[string]any) { section(m, g.key)["enabled"] = false }))
		})
		for _, l := range g.loosened {
			t.Run(fmt.Sprintf("%s/%s=%v", g.key, l.field, l.value), func(t *testing.T) {
				wantIn(t, mutate(t, func(m map[string]any) { section(m, g.key)[l.field] = l.value }))
			})
		}
	}
}

// Tightening is not a gap.
func TestSpecGapsAcceptsATighterFile(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(repoRoot(t), "ci", "quality.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Crap.Ceiling, cfg.Coverage.TotalFloor, cfg.Stryker.Break = 5, 90, 90
	cfg.Trivy.Severity = "MEDIUM,HIGH,CRITICAL"
	if gaps := specGaps(cfg); len(gaps) != 0 {
		t.Errorf("a tighter config has gaps: %v", gaps)
	}
}

// Every tracked file list must carry no baseline or exemption file.
func TestNoBaselineOrExemptionFileIsTracked(t *testing.T) {
	out, err := gitOutput(repoRoot(t), "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range strings.Fields(out) {
		base := strings.ToLower(filepath.Base(f))
		if strings.Contains(base, "baseline") || strings.Contains(base, "exempt") || strings.Contains(base, "exclusion") {
			t.Errorf("%s looks like a baseline or exemption file; there is none by policy", f)
		}
	}
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
