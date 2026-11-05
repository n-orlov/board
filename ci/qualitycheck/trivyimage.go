// The image Trivy gate (SPEC 15.4): HIGH and CRITICAL findings in the
// container image the root Dockerfile builds. The Jenkinsfile's Quality stage
// builds the image and saves it to ci-results/board-image.tar
// (ci/image-build.sh); this gate scans that tar. No tar means nothing was
// measured, so the gate fails instead of passing.
package main

import (
	"fmt"
	"os"
	"strings"
)

// trivyImageArgs builds the command line of a scan of the image tar: the
// same severity, unfixed-exclusion, exit code and exceptions file as the
// source scan.
func trivyImageArgs(o trivyOptions) []string {
	args := []string{"image",
		"--input", o.Image,
		"--scanners", "vuln,secret",
		"--severity", o.Severity,
		"--ignore-unfixed",
		"--exit-code", "1",
		"--cache-dir", o.CacheDir,
		"--skip-version-check",
		"--no-progress",
	}
	if _, err := os.Stat(o.IgnoreFile); err == nil {
		args = append(args, "--ignorefile", o.IgnoreFile)
	}
	return args
}

// runTrivyImageGate scans the image tar o.Image. ok=false with a nil error
// is a finding, an unacceptable exception or a missing tar; a non-nil error
// is a tooling failure, like runTrivyGate's.
func runTrivyImageGate(o trivyOptions) (ok bool, output string, err error) {
	if err := validateTrivySeverity(o.Severity); err != nil {
		return false, "", err
	}
	if o.CacheDir == "" {
		return false, "", fmt.Errorf("trivy-image gate: no cache dir")
	}
	if info, serr := os.Stat(o.Image); serr != nil || info.Size() == 0 {
		return false, fmt.Sprintf("trivy-image gate: FAILED, input missing or empty: %s\n"+
			"what to do: ci/image-build.sh builds the root Dockerfile and saves it there; "+
			"a gate with nothing to measure fails instead of passing.\n", o.Image), nil
	}
	problems, perr := checkTrivyIgnore(o.IgnoreFile, o.Now)
	if perr != nil {
		return false, "", fmt.Errorf("reading %s: %w", o.IgnoreFile, perr)
	}
	if len(problems) > 0 {
		return false, "trivy-image gate: .trivyignore is not acceptable:\n" + strings.Join(problems, "\n") +
			"\nwhat to do: every entry needs \"<ID> review-by:YYYY-MM-DD # <reason>\" with a date not yet passed.\n", nil
	}
	return runTrivyScan(o)
}
