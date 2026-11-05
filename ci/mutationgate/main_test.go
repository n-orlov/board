package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gremlinsReport builds a gremlins JSON report with the given mutants by status.
func gremlinsReport(killed, lived, timedOut, notCovered int) string {
	var ms []string
	add := func(n int, status string) {
		for i := 0; i < n; i++ {
			ms = append(ms, fmt.Sprintf(`{"type":"CONDITIONALS_NEGATION","status":%q,"line":%d,"column":1}`, status, i+1))
		}
	}
	add(killed, "KILLED")
	add(lived, "LIVED")
	add(timedOut, "TIMED OUT")
	add(notCovered, "NOT COVERED")
	return `{"go_module":"m","files":[{"file_name":"a.go","mutations":[` + strings.Join(ms, ",") + `]}]}`
}

const testConfig = `{"mutation_go":{"enabled":true,"efficacy":80,"packages":["internal/domain","internal/store"],"fail_on_no_mutants":true}}`

// gate runs the gate over a temp dir holding the given reports (report
// file name -> content) and returns the exit code and output.
func gate(t *testing.T, config string, reports map[string]string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "quality.json")
	if err := os.WriteFile(cfg, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range reports {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	code := run([]string{"-config", cfg, "-dir", dir}, &out, &errb)
	return code, out.String() + errb.String()
}

func TestGatePassesAtAndAboveTheFloor(t *testing.T) {
	code, out := gate(t, testConfig, map[string]string{
		"internal-domain.json": gremlinsReport(4, 1, 0, 0), // 80.00 %, exactly the floor
		"internal-store.json":  gremlinsReport(9, 1, 3, 2),
	})
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
}

func TestGateFailsOnAReportWithZeroMutants(t *testing.T) {
	code, out := gate(t, testConfig, map[string]string{
		"internal-domain.json": gremlinsReport(0, 0, 0, 0),
		"internal-store.json":  gremlinsReport(9, 1, 0, 0),
	})
	if code != 1 || !strings.Contains(out, "internal/domain: FAILED: gremlins generated no mutant") {
		t.Fatalf("exit %d, want 1 naming the zero-mutant package:\n%s", code, out)
	}
}

func TestGateFailsOnAReportBelowEightyPercentEfficacy(t *testing.T) {
	code, out := gate(t, testConfig, map[string]string{
		"internal-domain.json": gremlinsReport(3, 1, 0, 0), // 75 %
		"internal-store.json":  gremlinsReport(9, 1, 0, 0),
	})
	if code != 1 || !strings.Contains(out, "efficacy 75.00% is below the floor 80.00%") {
		t.Fatalf("exit %d, want 1 naming 75.00%%:\n%s", code, out)
	}
}

func TestGateFailsWhenEveryMutantIsNotCovered(t *testing.T) {
	code, out := gate(t, testConfig, map[string]string{
		"internal-domain.json": gremlinsReport(0, 0, 0, 5),
		"internal-store.json":  gremlinsReport(9, 1, 0, 0),
	})
	if code != 1 || !strings.Contains(out, "no mutant was judged") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestGateFailsOnAMissingOrMalformedReport(t *testing.T) {
	code, out := gate(t, testConfig, map[string]string{"internal-domain.json": gremlinsReport(9, 1, 0, 0)})
	if code != 1 || !strings.Contains(out, "internal/store: FAILED, report") {
		t.Fatalf("missing: exit %d:\n%s", code, out)
	}
	code, out = gate(t, testConfig, map[string]string{
		"internal-domain.json": gremlinsReport(9, 1, 0, 0),
		"internal-store.json":  "not json",
	})
	if code != 1 || !strings.Contains(out, "is not a gremlins report") {
		t.Fatalf("malformed: exit %d:\n%s", code, out)
	}
}

func TestGateRefusesADisabledOrPackagelessConfig(t *testing.T) {
	off := `{"mutation_go":{"enabled":false,"efficacy":80,"packages":["a"],"fail_on_no_mutants":true}}`
	if code, _ := gate(t, off, nil); code != 2 {
		t.Errorf("disabled gate: exit %d, want 2", code)
	}
	none := `{"mutation_go":{"enabled":true,"efficacy":80,"packages":[],"fail_on_no_mutants":true}}`
	if code, out := gate(t, none, nil); code != 1 || !strings.Contains(out, "no package is configured") {
		t.Errorf("no packages: exit %d: %s", code, out)
	}
}

func TestGateCountsFromMutationStatusesNotSummaryFields(t *testing.T) {
	// gremlins' summary fields omit timed-out and not-covered mutants; the
	// gate must tally the mutations themselves.
	c, err := tally([]byte(gremlinsReport(2, 1, 4, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Killed != 2 || c.Lived != 1 || c.TimedOut != 4 || c.NotCovered != 3 || c.total() != 10 {
		t.Fatalf("counts %+v", c)
	}
}

func TestListPrintsTheConfiguredPackagesAndUsageErrorsExitTwo(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "q.json")
	if err := os.WriteFile(cfg, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"-config", cfg, "-list"}, &out, &errb); code != 0 || out.String() != "internal/domain\ninternal/store\n" {
		t.Fatalf("list: exit %d %q", code, out.String())
	}
	if code := run(nil, &out, &errb); code != 2 {
		t.Errorf("no -config: exit %d, want 2", code)
	}
	if code := run([]string{"-config", filepath.Join(dir, "absent.json")}, &out, &errb); code != 2 {
		t.Errorf("absent config: exit %d, want 2", code)
	}
	if code := run([]string{"-bogus"}, &out, &errb); code != 2 {
		t.Errorf("bad flag: exit %d, want 2", code)
	}
}

func TestReportNameFlattensThePackagePath(t *testing.T) {
	if got := reportName("internal/domain"); got != "internal-domain.json" {
		t.Fatalf("got %q", got)
	}
	if got := reportName("./internal/store/"); got != "internal-store.json" {
		t.Fatalf("got %q", got)
	}
}
