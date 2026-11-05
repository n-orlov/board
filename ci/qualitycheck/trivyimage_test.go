package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// imageTar writes a non-empty stand-in for the image tar.
func imageTar(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "board-image.tar")
	if err := os.WriteFile(p, []byte("tar"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The image gate scans the tar with `trivy image --input`, at the
// configured severity, unfixed findings excluded, exiting 1 on a finding.
func TestTrivyImageGateRunsTrivyImageOnTheTar(t *testing.T) {
	bin, argvLog := fakeTrivy(t, 0)
	o := opts(t, bin, "")
	o.Image = imageTar(t)
	ok, out, err := runTrivyImageGate(o)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v out=%s", ok, err, out)
	}
	raw, _ := os.ReadFile(argvLog)
	joined := strings.ReplaceAll(strings.TrimSpace(string(raw)), "\n", " ")
	for _, want := range []string{"image --input " + o.Image, "--severity HIGH,CRITICAL", "--ignore-unfixed", "--exit-code 1", "--cache-dir " + o.CacheDir} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q lacks %q", joined, want)
		}
	}
}

func TestTrivyImageGateFindingFailsAndToolFailureIsAnError(t *testing.T) {
	o := opts(t, "", "")
	o.Image = imageTar(t)
	o.Binary, _ = fakeTrivy(t, 1)
	if ok, out, err := runTrivyImageGate(o); err != nil || ok || !strings.Contains(out, "fake trivy report") {
		t.Errorf("finding: ok=%v err=%v out=%q, want a failing gate carrying the report", ok, err, out)
	}
	o.Binary, _ = fakeTrivy(t, 2)
	if ok, _, err := runTrivyImageGate(o); err == nil || ok {
		t.Errorf("exit 2: ok=%v err=%v, want a tooling error", ok, err)
	}
}

// No tar, an empty tar, a severity below HIGH and an expired exception each
// fail the gate, and trivy never runs for the first two.
func TestTrivyImageGateFailsWithoutAnInputOrAnAcceptableConfig(t *testing.T) {
	bin, argvLog := fakeTrivy(t, 0)
	o := opts(t, bin, "")
	o.Image = filepath.Join(t.TempDir(), "missing.tar")
	if ok, out, err := runTrivyImageGate(o); err != nil || ok || !strings.Contains(out, "input missing or empty") {
		t.Errorf("missing tar: ok=%v err=%v out=%q", ok, err, out)
	}
	o.Image = filepath.Join(t.TempDir(), "empty.tar")
	if err := os.WriteFile(o.Image, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, out, err := runTrivyImageGate(o); err != nil || ok || !strings.Contains(out, "input missing or empty") {
		t.Errorf("empty tar: ok=%v err=%v out=%q", ok, err, out)
	}
	if _, err := os.Stat(argvLog); err == nil {
		t.Error("trivy ran although there was no image to scan")
	}
	o.Image = imageTar(t)
	o.Severity = "CRITICAL"
	if ok, _, err := runTrivyImageGate(o); err == nil || ok {
		t.Errorf("severity without HIGH: ok=%v err=%v, want an error", ok, err)
	}
	o.Severity = "HIGH,CRITICAL"
	writeIgnore(t, o, "CVE-2020-0001 review-by:2026-01-01 # expired\n")
	if ok, out, err := runTrivyImageGate(o); err != nil || ok || !strings.Contains(out, ".trivyignore") {
		t.Errorf("expired exception: ok=%v err=%v out=%q", ok, err, out)
	}
}

// -mutation-deferred makes the two mutation gates say where they are judged,
// without reading ci-results/mutation/, and leaves every other gate alone.
func TestMutationGatesDeferredToTheMutationStage(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := config{MutationGo: mutationGoConfig{Enabled: true}, Stryker: strykerConfig{Enabled: true}}
	run := func(deferred bool) []string {
		saved := mutationDeferred
		mutationDeferred = deferred
		defer func() { mutationDeferred = saved }()
		var lines []string
		for _, g := range newGates(cfg, "ci/quality.json") {
			ok, out, err := g.run()
			if err != nil {
				t.Fatalf("%s: %v", g.name, err)
			}
			lines = append(lines, g.name+" ok="+map[bool]string{true: "true", false: "false"}[ok]+" "+strings.Split(out, "\n")[0])
		}
		return lines
	}
	for _, l := range run(true) {
		if !strings.Contains(l, "ok=true") || !strings.Contains(l, "deferred to the Mutation stage") {
			t.Errorf("deferred: %q, want a pass naming the Mutation stage", l)
		}
	}
	for _, l := range run(false) {
		if !strings.Contains(l, "ok=false") {
			t.Errorf("not deferred, no reports: %q, want a failing gate", l)
		}
	}
}
