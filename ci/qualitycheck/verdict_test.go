package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// verdictLines returns, per "=== name gate ===" section of a report, the
// section's `gate verdict:` lines.
func verdictLines(report string) map[string][]string {
	out := map[string][]string{}
	name := ""
	for _, line := range strings.Split(report, "\n") {
		if m := regexp.MustCompile(`^=== (.*) gate ===$`).FindStringSubmatch(line); m != nil {
			name = m[1]
			out[name] = nil
			continue
		}
		if strings.HasPrefix(line, "gate verdict: ") {
			out[name] = append(out[name], line)
		}
	}
	return out
}

func TestEverySectionEndsInExactlyOneVerdictLineDerivedFromOkAndDeferred(t *testing.T) {
	results := []gateResult{
		{name: "crap", ok: true, output: "crap gate: ok\nwhat to do: nothing\n"},
		{name: "coverage", ok: false, output: "coverage gate: below the floor\nwhat to do: nothing"},
		{name: "stryker", ok: true, deferred: true, output: "stryker gate: deferred\n"},
		// A failed gate that is also marked deferred is still failed.
		{name: "odd", ok: false, deferred: true, output: "odd gate: broken\n"},
	}
	text, overallOK := renderGateReport(results)
	if overallOK {
		t.Error("overallOK = true with a failed gate")
	}
	want := map[string]string{"crap": "passed", "coverage": "failed", "stryker": "deferred", "odd": "failed"}
	got := verdictLines(text)
	for name, verdict := range want {
		if len(got[name]) != 1 || got[name][0] != "gate verdict: "+verdict {
			t.Errorf("%s: verdict lines %q, want exactly %q", name, got[name], "gate verdict: "+verdict)
		}
	}
	// The verdict line is the last line of its section, whatever the body says.
	sections := strings.Split(text, "=== ")[1:]
	for _, s := range sections {
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		if !strings.HasPrefix(lines[len(lines)-1], "gate verdict: ") {
			t.Errorf("section %q does not end in its verdict line", lines[0])
		}
	}
}

func TestADeferredGateDoesNotFailTheRun(t *testing.T) {
	text, overallOK := renderGateReport([]gateResult{
		{name: "crap", ok: true},
		{name: "mutation-go", ok: true, deferred: true, output: "deferred\n"},
	})
	if !overallOK {
		t.Errorf("a passed and a deferred gate made overallOK false:\n%s", text)
	}
	if v := verdictLines(text); v["crap"][0] != "gate verdict: passed" || v["mutation-go"][0] != "gate verdict: deferred" {
		t.Errorf("verdicts %v", v)
	}
}

// With -mutation-deferred, run reports mutation-go and stryker deferred and
// exits 0; without it a missing Mutation report fails them.
func TestMutationDeferredRunGivesDeferredVerdictsAndExitsZero(t *testing.T) {
	root := repoRoot(t)
	cfg := filepath.Join(t.TempDir(), "quality.json")
	writeFile(t, cfg, `{"mutation_go":{"enabled":true,"packages":["internal/domain"],"efficacy":80},`+
		`"stryker":{"enabled":true,"break":80,"module_break":60}}`)
	t.Chdir(root)

	saved := mutationDeferred
	defer func() { mutationDeferred = saved }()

	mutationDeferred = true
	report, code, err := run(cfg, "")
	if err != nil || code != 0 {
		t.Fatalf("deferred run: code=%d err=%v\n%s", code, err, report)
	}
	got := verdictLines(report)
	for _, name := range []string{"mutation-go", "stryker"} {
		if len(got[name]) != 1 || got[name][0] != "gate verdict: deferred" {
			t.Errorf("%s under -mutation-deferred: %q, want gate verdict: deferred", name, got[name])
		}
	}

	mutationDeferred = false
	t.Chdir(t.TempDir())
	report, code, err = run(cfg, "")
	if err != nil || code != 1 {
		t.Fatalf("not deferred, no reports: code=%d err=%v\n%s", code, err, report)
	}
	for name, lines := range verdictLines(report) {
		if len(lines) != 1 || lines[0] != "gate verdict: failed" {
			t.Errorf("%s with no mutation reports: %q, want gate verdict: failed", name, lines)
		}
	}
}

// The deferred text names the real ways a build judges mutation (MUTATION=true,
// MUTATION_FULL=true, the nightly timer) and makes no claim about an extended
// lane: EXTENDED alone runs no mutation.
func TestDeferredMessageNamesTheMutationTriggersAndNoExtendedLane(t *testing.T) {
	for _, name := range []string{"mutation-go", "stryker"} {
		ok, out, err := deferredToMutationStage(name)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", name, ok, err)
		}
		for _, want := range []string{"MUTATION=true", "MUTATION_FULL=true", "nightly timer", name + " gate"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s deferred text %q lacks %q", name, out, want)
			}
		}
		for _, bad := range []string{"extended lane", "EXTENDED=true", "EXTENDED"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s deferred text %q still claims %q", name, out, bad)
			}
		}
	}
}
