package repocheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ci/mutation-merge.sh judges the full-mode units' union: it copies every
// unit's reports into one outdir, fails a report two units both wrote, takes
// Stryker's verdict from the one unit that ran it, and runs ci/mutationgate
// over lane all. These tests run it with a stub go (the gate's verdict is
// STUB_GATE) over hand-made unit outdirs.

const stubGateGo = `#!/bin/sh
echo "stub gate: $*"
exit "${STUB_GATE:-0}"
`

type mergeUnit struct {
	reports []string
	stryker string // the unit's "stryker:" verdict line; "" writes no verdict file
}

// runMerge returns the merge's exit status and the unit-verdict.txt it wrote.
func runMerge(t *testing.T, gate string, units ...mergeUnit) (int, string) {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stubGateGo), 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	args := []string{filepath.Join(root, "ci", "mutation-merge.sh"), filepath.Join(work, "out"), "ci/quality.json"}
	for i, u := range units {
		dir := filepath.Join(work, "unit"+string(rune('a'+i)))
		if err := os.MkdirAll(filepath.Join(dir, "stryker"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, r := range u.reports {
			if err := os.WriteFile(filepath.Join(dir, r+".json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if u.stryker != "" {
			if err := os.WriteFile(filepath.Join(dir, "unit-verdict.txt"), []byte("mutation-go: passed\nstryker: "+u.stryker+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if u.stryker != "not run" {
				if err := os.WriteFile(filepath.Join(dir, "stryker", "mutation.json"), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		args = append(args, dir)
	}
	args = append(args, filepath.Join(work, "missing"))
	cmd := exec.Command("sh", args...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + t.TempDir(), "STUB_GATE=" + gate}
	out, err := cmd.CombinedOutput()
	status := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		status = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("ci/mutation-merge.sh: %v\n%s", err, out)
	}
	verdict, err := os.ReadFile(filepath.Join(work, "out", "unit-verdict.txt"))
	if err != nil {
		t.Fatalf("no unit-verdict.txt: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "stub gate: run ./ci/mutationgate -config ci/quality.json -dir "+filepath.Join(work, "out")+" -lane all") {
		t.Errorf("the gate did not run over lane all of the merged outdir:\n%s", out)
	}
	return status, string(verdict)
}

func TestMutationMergeJudgesTheUnion(t *testing.T) {
	for name, c := range map[string]struct {
		gate    string
		units   []mergeUnit
		status  int
		verdict string
	}{
		"every unit green": {"0", []mergeUnit{{[]string{"a", "b"}, "not run"}, {[]string{"c"}, "passed"}}, 0,
			"mutation-go: passed\nstryker: passed\n"},
		"a red package": {"1", []mergeUnit{{[]string{"a"}, "not run"}, {[]string{"c"}, "passed"}}, 1,
			"mutation-go: failed\nstryker: passed\n"},
		"a report in two units": {"0", []mergeUnit{{[]string{"a"}, "not run"}, {[]string{"a"}, "passed"}}, 1,
			"mutation-go: failed\nstryker: passed\n"},
		"red Stryker": {"0", []mergeUnit{{[]string{"a"}, "not run"}, {[]string{"c"}, "failed"}}, 1,
			"mutation-go: passed\nstryker: failed\n"},
		"Stryker in no unit": {"0", []mergeUnit{{[]string{"a"}, "not run"}, {[]string{"c"}, ""}}, 1,
			"mutation-go: passed\nstryker: failed\n"},
		"Stryker in two units": {"0", []mergeUnit{{[]string{"a"}, "passed"}, {[]string{"c"}, "passed"}}, 1,
			"mutation-go: passed\nstryker: failed\n"},
	} {
		status, verdict := runMerge(t, c.gate, c.units...)
		if status != c.status || verdict != c.verdict {
			t.Errorf("%s: status %d, verdict %q; want %d, %q", name, status, verdict, c.status, c.verdict)
		}
	}
}
