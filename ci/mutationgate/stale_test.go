package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// staleModule is a module where internal/a imports internal/b, internal/c's
// tests import internal/h, and internal/c is split into two parts.
func staleModule() map[string]*string {
	return map[string]*string{
		"go.mod":                    src("module example.com/m\n\ngo 1.21\n"),
		"go.sum":                    src(""),
		"internal/a/a.go":           src("package a\n\nimport \"example.com/m/internal/b\"\n\nfunc A() int { return b.B() }\n"),
		"internal/b/b.go":           src("package b\n\nfunc B() int { return 1 }\n"),
		"internal/b/testdata/x.txt": src("x\n"),
		"internal/c/c1.go":          src("package c\n\nfunc C1() int { return 1 }\n"),
		"internal/c/c2.go":          src("package c\n\nfunc C2() int { return 2 }\n"),
		"internal/c/c_test.go":      src("package c\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/internal/h\"\n)\n\nfunc TestC(t *testing.T) { _ = h.H() }\n"),
		"internal/h/h.go":           src("package h\n\nfunc H() int { return 0 }\n"),
		"ci/quality.json":           src(staleConfig("p1")),
	}
}

func staleConfig(part string) string {
	return `{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,
	  "packages":["internal/a","internal/b","internal/c"],
	  "scopes":{"internal/c":["c1.go","c2.go"]},
	  "parts":{"internal/c":{"` + part + `":["c1.go"],"p2":["c2.go"]}}}}`
}

// runStale commits staleModule, then commits change over it, and runs -stale
// against the first commit.
func runStale(t *testing.T, change map[string]*string) (int, string, string) {
	t.Helper()
	root, base := phaseRepo(t, staleModule(), change)
	t.Chdir(root)
	var out, errb bytes.Buffer
	code := run([]string{"-config", "ci/quality.json", "-stale", base}, &out, &errb)
	return code, out.String(), errb.String()
}

func TestStaleListsTheUnitsAChangeCanReach(t *testing.T) {
	cases := []struct {
		name   string
		change map[string]*string
		want   string
	}{
		{"nothing of the units changed", map[string]*string{"notes.txt": src("n\n")}, ""},
		{"a dependency's code", map[string]*string{"internal/b/b.go": src("package b\n\nfunc B() int { return 2 }\n")}, "internal/a\ninternal/b\n"},
		{"a dependency's testdata", map[string]*string{"internal/b/testdata/x.txt": src("y\n")}, "internal/b\n"},
		{"a dependency's test file", map[string]*string{"internal/b/b_test.go": src("package b\n")}, "internal/b\n"},
		{"a package only its tests import", map[string]*string{"internal/h/h.go": src("package h\n\nfunc H() int { return 1 }\n")}, "internal/c#p1\ninternal/c#p2\n"},
		{"a deleted test file", map[string]*string{"internal/c/c_test.go": nil}, "internal/c#p1\ninternal/c#p2\n"},
		{"the config's thresholds only", map[string]*string{"ci/quality.json": src(strings.Replace(staleConfig("p1"), `"efficacy":80`, `"efficacy":85`, 1))}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errs := runStale(t, tc.change)
			if code != 0 || out != tc.want {
				t.Fatalf("exit %d, printed %q, want %q\n%s", code, out, tc.want, errs)
			}
		})
	}
}

func TestStaleExitsThreeWhenEveryUnitMustRun(t *testing.T) {
	cases := map[string]map[string]*string{
		"go.sum changed":            {"go.sum": src("example.com/x v1.0.0 h1:x=\n")},
		"ci/mutation.sh changed":    {"ci/mutation.sh": src("#!/bin/sh\n")},
		"packages, scopes or parts": {"ci/quality.json": src(staleConfig("q1"))},
	}
	for want, change := range cases {
		t.Run(want, func(t *testing.T) {
			code, out, errs := runStale(t, change)
			if code != 3 || out != "" || !strings.Contains(errs, "mutationgate: full: ") || !strings.Contains(errs, want) {
				t.Fatalf("exit %d, printed %q, stderr %q: want 3 naming %q", code, out, errs, want)
			}
		})
	}
}

func TestStaleExitsTwoOnABadRefOrPackage(t *testing.T) {
	code, _, errs := runStale(t, map[string]*string{"notes.txt": src("n\n")})
	if code != 0 {
		t.Fatalf("setup: exit %d %s", code, errs)
	}
	var out, errb bytes.Buffer
	if c := run([]string{"-config", "ci/quality.json", "-stale", "no-such-ref"}, &out, &errb); c != 2 || !strings.Contains(errb.String(), "git diff") {
		t.Fatalf("bad ref: exit %d %q", c, errb.String())
	}
	if _, err := goListDeps(t.TempDir(), "internal/nope"); err == nil || !strings.Contains(err.Error(), "go list") {
		t.Fatalf("go list outside a module: %v", err)
	}
	var full fullRunError
	if err := unitsChanged(config{}, ".", "ci/none.json", "HEAD"); !errors.As(err, &full) || !strings.Contains(full.reason, "unreadable at HEAD") {
		t.Fatalf("a config missing at ref: %v", err)
	}
	broken := func(string, string) ([]string, error) { return nil, errors.New("boom") }
	if _, err := staleUnits(config{}, ".", "", "HEAD", []string{"internal/a"}, broken); err == nil || err.Error() != "boom" {
		t.Fatalf("deps error: %v", err)
	}
}

func TestRepoPathAndReaches(t *testing.T) {
	for p, want := range map[string]string{"/r": ".", "/r/ci/q.json": "ci/q.json", "ci/q.json": "ci/q.json", "/elsewhere/q.json": ""} {
		if got := repoPath("/r", p); got != want {
			t.Errorf("repoPath(/r, %s) = %q, want %q", p, got, want)
		}
	}
	if !reaches("main.go", "internal/a", []string{"."}) || reaches("internal/x.go", "internal/a", []string{"."}) {
		t.Error("the module root's package owns only root-level files")
	}
	if reaches("internal/ab/x.go", "internal/b", []string{"internal/a"}) {
		t.Error("internal/ab is not below internal/a")
	}
	if got := (fullRunError{"x"}).Error(); got != "every unit must run: x" {
		t.Errorf("fullRunError: %q", got)
	}
}

func TestALaneListMayNameSingleUnits(t *testing.T) {
	code, out := laneRun(t, lanedConfig, []string{"-list", "-lane", "internal/server,base"}, nil)
	if code != 0 || out != "internal/domain\ninternal/store\ninternal/server\n" {
		t.Fatalf("unit plus lane: exit %d %q", code, out)
	}
	if code, out = laneRun(t, lanedConfig, []string{"-list", "-lane", "internal/nope"}, nil); code != 2 || !strings.Contains(out, "unknown lane \"internal/nope\"") {
		t.Fatalf("unknown unit: exit %d %q", code, out)
	}
}
