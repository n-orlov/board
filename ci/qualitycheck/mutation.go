// The Go mutation gate's place in ci/quality.sh: it evaluates the gremlins
// reports ci/mutation.sh wrote (the Mutation stage runs that script, which
// is the gate's own entry point) with ci/mutationgate. No report means no
// measurement, so the gate fails instead of passing.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// mutationReportDir is where ci/mutation.sh writes its reports, under the
// suite's output directory.
func mutationReportDir(outdir string) string {
	return filepath.Join(outdir, "mutation")
}

// runMutationGate runs ci/mutationgate over the reports in dir.
func runMutationGate(configPath, dir string) (ok bool, output string, err error) {
	if _, serr := os.Stat(dir); serr != nil {
		return false, fmt.Sprintf("mutation-go gate: FAILED, input missing: %s\n"+
			"what to do: ci/mutation.sh produces it; a gate with nothing to measure fails instead of passing.\n", dir), nil
	}
	text, code, err := goRunTool("./ci/mutationgate", "-config", configPath, "-dir", dir)
	if err != nil {
		return false, text, err
	}
	return code == 0, text, nil
}
