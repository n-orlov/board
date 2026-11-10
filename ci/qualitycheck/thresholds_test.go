package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// marshalConfig is a test-only helper: it JSON-marshals cfg and writes
// it to a temp file via writeTempFile, so seeded configs in this file
// are built from the real config struct (and its json tags) rather
// than hand-typed literals that could silently drift from the shape
// loadConfig actually parses.
func marshalConfig(t *testing.T, cfg config) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshalling seeded config: %v", err)
	}
	return writeTempFile(t, string(data))
}

// baselineConfig is a config with every threshold at the checked-in
// ci/quality.json's own values (see that file), used as the "base"
// side of every seeded comparison test below.
func baselineConfig() config {
	return config{
		Coverage: coverageConfig{Enabled: true, TotalFloor: 85, PackageFloor: 80, FixtureFloor: 50},
		Crap:     crapConfig{Enabled: true, Ceiling: 8, FixtureCeiling: 8},
		Trivy:    trivyConfig{Enabled: true, Severity: "HIGH,CRITICAL"},
	}
}

// TestThresholdsNotLoosened is R187's loosening test (criterion 1): it
// compares the real, checked-in ci/quality.json against the
// base-branch copy resolved exactly as R187 specifies (git merge-base
// HEAD origin/main, falling back to HEAD~1 on a shallow clone), via the
// real loadBaseConfig / resolveBaseRef git plumbing -- no stub here.
// Tightening always passes; this only fails if the checked-in file
// ever loosens relative to base.
func TestThresholdsNotLoosened(t *testing.T) {
	root := repoRoot(t)
	currentPath := root + "/ci/quality.json"
	if err := checkThresholdsNotLoosened(currentPath, func() (config, error) {
		return loadBaseConfig(root)
	}); err != nil {
		t.Fatal(err)
	}
}

// TestThresholdsNotLoosened_NoBaseCopyFailsNeverSkips proves criterion
// 2: when the base-config resolver cannot produce a base copy at all
// (R187's "merge-base cannot be resolved" case, e.g. a shallow clone
// with no origin/main reachable even after the HEAD~1 fallback),
// checkThresholdsNotLoosened returns a non-nil error -- it has no path
// that calls t.Skip, so this is structurally impossible to turn into a
// skip by accident.
func TestThresholdsNotLoosened_NoBaseCopyFailsNeverSkips(t *testing.T) {
	currentPath := marshalConfig(t, baselineConfig())
	stubErr := errors.New("stubbed: git merge-base HEAD origin/main failed, and the shallow-clone fallback git rev-parse HEAD~1 also failed")

	err := checkThresholdsNotLoosened(currentPath, func() (config, error) {
		return config{}, stubErr
	})
	if err == nil {
		t.Fatal("checkThresholdsNotLoosened with a resolver that cannot produce a base copy: want a non-nil error (fail, never skip), got nil")
	}
	if !errors.Is(err, stubErr) {
		t.Errorf("error = %v, want it to wrap the resolver's own error so the message stays clear", err)
	}
}

// TestThresholdsNotLoosened_SeededLoosenedFloorFails proves criterion 3
// (the "fails" half): a copy that lowers a coverage floor relative to
// base is loosened and must fail.
func TestThresholdsNotLoosened_SeededLoosenedFloorFails(t *testing.T) {
	base := baselineConfig()
	loosened := base
	loosened.Coverage.TotalFloor = 80 // was 85: a lower floor is looser.
	currentPath := marshalConfig(t, loosened)

	err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil })
	if err == nil {
		t.Fatal("want an error for a lowered coverage.total_floor, got nil")
	}
}

// TestThresholdsNotLoosened_SeededRaisedCeilingFails proves criterion 3
// for the CRAP side: a raised ceiling is looser and must fail.
func TestThresholdsNotLoosened_SeededRaisedCeilingFails(t *testing.T) {
	base := baselineConfig()
	loosened := base
	loosened.Crap.Ceiling = 20 // was 8: a higher ceiling is looser.
	currentPath := marshalConfig(t, loosened)

	err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil })
	if err == nil {
		t.Fatal("want an error for a raised crap.ceiling, got nil")
	}
}

// TestThresholdsNotLoosened_SeededGateSwitchedOffFails proves criterion
// 3 for "a gate is switched off": R187 treats disabling a gate base had
// on as a loosening even if every numeric field is untouched.
func TestThresholdsNotLoosened_SeededGateSwitchedOffFails(t *testing.T) {
	base := baselineConfig()
	loosened := base
	loosened.Crap.Enabled = false
	currentPath := marshalConfig(t, loosened)

	err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil })
	if err == nil {
		t.Fatal("want an error for a gate switched off relative to base, got nil")
	}
}

// TestThresholdsNotLoosened_SeededTightenedCopyPasses proves criterion
// 3 (the "passes" half): raising every floor and lowering every
// ceiling relative to base is tightening, which always passes.
func TestThresholdsNotLoosened_SeededTightenedCopyPasses(t *testing.T) {
	base := baselineConfig()
	tightened := config{
		Coverage: coverageConfig{Enabled: true, TotalFloor: 90, PackageFloor: 85, FixtureFloor: 55},
		Crap:     crapConfig{Enabled: true, Ceiling: 8, FixtureCeiling: 8},
		Trivy:    trivyConfig{Enabled: true, Severity: "MEDIUM,HIGH,CRITICAL"},
	}
	currentPath := marshalConfig(t, tightened)

	err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil })
	if err != nil {
		t.Fatalf("want nil for a tightened copy, got %v", err)
	}
}

// TestThresholdsNotLoosened_SeededIdenticalCopyPasses proves an
// unchanged copy (equal, not strictly tighter, on every field) passes
// -- the comparison is "no looser", not "strictly tighter".
func TestThresholdsNotLoosened_SeededIdenticalCopyPasses(t *testing.T) {
	base := baselineConfig()
	currentPath := marshalConfig(t, base)

	err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil })
	if err != nil {
		t.Fatalf("want nil for an identical copy, got %v", err)
	}
}

// TestThresholdsNotLoosened_MissingCurrentFileFails proves a missing
// current ci/quality.json is a failure (surfaced through loadConfig's
// own usage error), never a silent pass.
func TestThresholdsNotLoosened_MissingCurrentFileFails(t *testing.T) {
	err := checkThresholdsNotLoosened("/does/not/exist/quality.json", func() (config, error) {
		return baselineConfig(), nil
	})
	if err == nil {
		t.Fatal("want an error when the current ci/quality.json is missing, got nil")
	}
}

// crapCeilingConfig is baselineConfig with the CRAP ceiling and fixture
// ceiling both set to n.
func crapCeilingConfig(n float64) config {
	c := baselineConfig()
	c.Crap.Ceiling = n
	c.Crap.FixtureCeiling = n
	return c
}

// TestThresholdsNotLoosened_CrapTwentyToFifteenPasses: the R193 stage, a
// base at 20 and a checked-in copy at 15 is a tightening and passes.
func TestThresholdsNotLoosened_CrapTwentyToFifteenPasses(t *testing.T) {
	base := crapCeilingConfig(20)
	currentPath := marshalConfig(t, crapCeilingConfig(15))

	if err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil }); err != nil {
		t.Fatalf("tightening the CRAP ceiling 20 -> 15 must pass, got %v", err)
	}
}

// TestThresholdsNotLoosened_CrapFifteenToTwentyFails: the reverse move, a
// base at 15 and a copy back at 20, is a loosening and must fail.
func TestThresholdsNotLoosened_CrapFifteenToTwentyFails(t *testing.T) {
	base := crapCeilingConfig(15)
	currentPath := marshalConfig(t, crapCeilingConfig(20))

	if err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil }); err == nil {
		t.Fatal("loosening the CRAP ceiling 15 -> 20 must fail, got nil")
	}
}

// TestThresholdsNotLoosened_CrapFifteenToTenPasses: the final R193 stage, a
// base at 15 and a checked-in copy at 10 is a tightening and passes.
func TestThresholdsNotLoosened_CrapFifteenToTenPasses(t *testing.T) {
	base := crapCeilingConfig(15)
	currentPath := marshalConfig(t, crapCeilingConfig(10))

	if err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil }); err != nil {
		t.Fatalf("tightening the CRAP ceiling 15 -> 10 must pass, got %v", err)
	}
}

// TestThresholdsNotLoosened_CrapTenToFifteenFails: the reverse move, a base
// at 10 and a copy back at 15, is a loosening and must fail.
func TestThresholdsNotLoosened_CrapTenToFifteenFails(t *testing.T) {
	base := crapCeilingConfig(10)
	currentPath := marshalConfig(t, crapCeilingConfig(15))

	if err := checkThresholdsNotLoosened(currentPath, func() (config, error) { return base, nil }); err == nil {
		t.Fatal("loosening the CRAP ceiling 10 -> 15 must fail, got nil")
	}
}

// TestLooserThresholdsCatchesTheSection154Gates: each gate beyond deck's
// five, loosened against a base that has it, is reported; a base without
// the gate (the commit that adds it) and a tighter file are not.
func TestLooserThresholdsCatchesTheSection154Gates(t *testing.T) {
	on := config{
		CrapTS:           crapTSConfig{Enabled: true, Ceiling: 8},
		FrontendCoverage: frontendCoverageConfig{Enabled: true, TotalFloor: 85, FileFloor: 80},
		TrivyImage:       trivyImageConfig{Enabled: true, Severity: "HIGH,CRITICAL"},
		MutationGo:       mutationGoConfig{Enabled: true, Efficacy: 80, Packages: []string{"a", "b"}, FailOnNoMutants: true},
		Stryker:          strykerConfig{Enabled: true, Break: 80, ModuleBreak: 80, Path: "web/src/lib"},
		SBOMModule:       switchConfig{Enabled: true}, SBOMFrontend: switchConfig{Enabled: true}, SBOMImage: switchConfig{Enabled: true},
		OpenAPILint: switchConfig{Enabled: true}, OpenAPIDrift: switchConfig{Enabled: true},
	}
	loosen := map[string]func(c *config){
		"crap_ts off":          func(c *config) { c.CrapTS.Enabled = false },
		"crap_ts ceiling":      func(c *config) { c.CrapTS.Ceiling = 9 },
		"frontend off":         func(c *config) { c.FrontendCoverage.Enabled = false },
		"frontend total":       func(c *config) { c.FrontendCoverage.TotalFloor = 84 },
		"frontend file":        func(c *config) { c.FrontendCoverage.FileFloor = 79 },
		"image severity":       func(c *config) { c.TrivyImage.Severity = "CRITICAL" },
		"image off":            func(c *config) { c.TrivyImage.Enabled = false },
		"mutation off":         func(c *config) { c.MutationGo.Enabled = false },
		"mutation efficacy":    func(c *config) { c.MutationGo.Efficacy = 79 },
		"mutation package":     func(c *config) { c.MutationGo.Packages = []string{"a"} },
		"mutation zero":        func(c *config) { c.MutationGo.FailOnNoMutants = false },
		"stryker off":          func(c *config) { c.Stryker.Enabled = false },
		"stryker break":        func(c *config) { c.Stryker.Break = 79 },
		"stryker module break": func(c *config) { c.Stryker.ModuleBreak = 79 },
		"stryker path":         func(c *config) { c.Stryker.Path = "web/src" },
		"sbom module":          func(c *config) { c.SBOMModule.Enabled = false },
		"sbom frontend":        func(c *config) { c.SBOMFrontend.Enabled = false },
		"sbom image":           func(c *config) { c.SBOMImage.Enabled = false },
		"openapi lint":         func(c *config) { c.OpenAPILint.Enabled = false },
		"openapi drift":        func(c *config) { c.OpenAPIDrift.Enabled = false },
	}
	for name, edit := range loosen {
		t.Run(name, func(t *testing.T) {
			cur := on
			cur.MutationGo.Packages = append([]string(nil), on.MutationGo.Packages...)
			edit(&cur)
			if len(looserThresholds(on, cur)) == 0 {
				t.Errorf("loosening %q was not reported", name)
			}
			if p := looserThresholds(config{}, cur); len(p) != 0 {
				t.Errorf("a gate the base lacks is not a loosening, got %v", p)
			}
		})
	}
	if p := looserThresholds(on, on); len(p) != 0 {
		t.Errorf("an unchanged file reported %v", p)
	}
}

// TestLooserThresholdsCatchesANarrowedMutationScope: a package the base
// mutated whole may not be narrowed to some files, and a scope may not drop a
// file the base listed; a package the base did not measure may be scoped, and
// a wider scope is not a loosening.
func TestLooserThresholdsCatchesANarrowedMutationScope(t *testing.T) {
	mk := func(pkgs []string, scopes map[string][]string) config {
		return config{MutationGo: mutationGoConfig{Enabled: true, Efficacy: 80, Packages: pkgs, FailOnNoMutants: true, Scopes: scopes}}
	}
	whole := mk([]string{"a", "b"}, nil)
	scoped := mk([]string{"a", "b"}, map[string][]string{"b": {"x.go", "y.go"}})
	if p := looserThresholds(whole, scoped); len(p) != 1 || !strings.Contains(p[0], `"b" is narrowed`) {
		t.Errorf("narrowing a whole package: %v", p)
	}
	fewer := mk([]string{"a", "b"}, map[string][]string{"b": {"x.go"}})
	if p := looserThresholds(scoped, fewer); len(p) != 1 || !strings.Contains(p[0], "dropped y.go") {
		t.Errorf("dropping a scoped file: %v", p)
	}
	if p := looserThresholds(fewer, scoped); len(p) != 0 {
		t.Errorf("a wider scope reported %v", p)
	}
	if p := looserThresholds(mk([]string{"a"}, nil), scoped); len(p) != 0 {
		t.Errorf("scoping a package the base did not measure reported %v", p)
	}
	if p := looserThresholds(scoped, scoped); len(p) != 0 {
		t.Errorf("an unchanged scope reported %v", p)
	}
}

// TestLooserThresholdsCatchesARemovedPhaseBase: without phase_base the check
// that every file the phase changed is measured has nothing to diff from, so
// removing it loosens the gate; setting or moving it does not.
func TestLooserThresholdsCatchesARemovedPhaseBase(t *testing.T) {
	mk := func(base string) config {
		return config{MutationGo: mutationGoConfig{Enabled: true, Efficacy: 80, Packages: []string{"a"}, FailOnNoMutants: true, PhaseBase: base}}
	}
	if p := looserThresholds(mk("abc"), mk("")); len(p) != 1 || !strings.Contains(p[0], "mutation_go.phase_base: removed") {
		t.Errorf("removing phase_base: %v", p)
	}
	for _, tc := range [][2]string{{"", "abc"}, {"abc", "def"}, {"abc", "abc"}} {
		if p := looserThresholds(mk(tc[0]), mk(tc[1])); len(p) != 0 {
			t.Errorf("phase_base %q -> %q reported %v", tc[0], tc[1], p)
		}
	}
}
