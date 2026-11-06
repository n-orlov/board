// Package features is the covfixture's stand-in for the real Godog entry
// point (task 002, R187): a no-op TestFeatures, fast enough that
// ci/suite.sh's second pass contributes nothing to this fixture's
// coverage data either -- which is the point, see unit/unit_test.go's own
// header. Test files are never instrumented, so the marker below keeps the
// fixture statement-free.
package features

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// extraFlagMarker is set by extraflag_test.go, which only builds under
// -tags=suiteextraflag.
var extraFlagMarker string

// emitCoverage stands in for the real harness's instrumented board binary:
// it builds probe/ with -cover in the mode the suite chose and runs it with
// GOCOVERDIR, so the leg leaves real counter data.
func emitCoverage(t *testing.T) {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		t.Fatal("the suite supplied no GOCOVERDIR")
	}
	bin := filepath.Join(t.TempDir(), "probe")
	args := []string{"build", "-o", bin, "-cover"}
	if mode := os.Getenv("BOARD_FEATURES_COVERMODE"); mode != "" {
		args = append(args, "-covermode="+mode)
	}
	args = append(args, "boardci.fixture/cov/probe")
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("building the probe: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Env = append(os.Environ(), "GOCOVERDIR="+dir)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("running the probe: %v\n%s", err, out)
	}
}

func TestFeatures(t *testing.T) {
	// FIXTURE_SCENARIOS=1 makes this pass look like a Godog run that executed
	// scenarios (the summary line Godog prints), FIXTURE_COVERAGE_LEGS names
	// the database legs whose "scenarios" leave coverage counters.
	if os.Getenv("FIXTURE_SCENARIOS") == "1" {
		fmt.Println("1 scenarios (1 passed)")
		legs := strings.Fields(os.Getenv("FIXTURE_COVERAGE_LEGS"))
		for _, leg := range legs {
			if leg == os.Getenv("BOARD_DB_LEG") {
				emitCoverage(t)
			}
		}
	}
	dir := os.Getenv("EXTRA_PROBE_DIR")
	if dir != "" {
		// What ci/suite.sh told this pass about the unit pass's covermode.
		if err := os.WriteFile(filepath.Join(dir, "features-covermode"), []byte(os.Getenv("BOARD_FEATURES_COVERMODE")), 0o644); err != nil {
			t.Fatalf("write covermode marker: %v", err)
		}
	}
	if dir == "" || extraFlagMarker == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "features"), []byte(extraFlagMarker), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}
