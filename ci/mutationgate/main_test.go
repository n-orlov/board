package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
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

// scopedConfig narrows internal/server to two files and leaves internal/store whole.
const scopedConfig = `{"mutation_go":{"enabled":true,"efficacy":80,"packages":["internal/server","internal/store"],"fail_on_no_mutants":true,
"scopes":{"internal/server":["askops.go","noteops.go"]}}}`

// moduleWith writes a module root holding the named files under pkg and a
// quality.json, and returns the root and the config path.
func moduleWith(t *testing.T, config, pkg string, files ...string) (root, cfg string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, pkg), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, pkg, f), []byte("package x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(root, "quality.json")
	if err := os.WriteFile(cfg, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func TestExcludesLeaveOutEveryNonTestFileOutsideTheScope(t *testing.T) {
	root, cfg := moduleWith(t, scopedConfig, "internal/server",
		"askops.go", "noteops.go", "taskops.go", "login.go", "server_test.go", "notes.txt")
	c, err := loadConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := excludes(c, root, "internal/server")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`^login\.go$`, `^taskops\.go$`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("excludes %q, want %q", got, want)
	}
	// The regexps match the out-of-scope files by path and never the scoped ones.
	for _, f := range []string{"taskops.go", "login.go"} {
		if !regexp.MustCompile(got[0]).MatchString(f) && !regexp.MustCompile(got[1]).MatchString(f) {
			t.Errorf("no exclude matches %s", f)
		}
	}
	for _, f := range []string{"askops.go", "noteops.go", "mytaskops.go"} {
		for _, re := range got {
			if regexp.MustCompile(re).MatchString(f) {
				t.Errorf("%s matches %s but is in scope or a different file", re, f)
			}
		}
	}
}

func TestAPackageWithoutAScopeIsMutatedWhole(t *testing.T) {
	root, cfg := moduleWith(t, scopedConfig, "internal/store", "store.go")
	c, err := loadConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := excludes(c, root, "./internal/store/")
	if err != nil || len(got) != 0 {
		t.Fatalf("excludes %q, err %v; want none", got, err)
	}
}

func TestAScopeNamingAMissingFileOrNoFileIsRefused(t *testing.T) {
	root, cfg := moduleWith(t, scopedConfig, "internal/server", "askops.go")
	c, err := loadConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := excludes(c, root, "internal/server"); err == nil || !strings.Contains(err.Error(), "noteops.go") {
		t.Errorf("a scope naming an absent file: err %v", err)
	}
	c.Scopes["internal/server"] = nil
	if _, err := excludes(c, root, "internal/server"); err == nil || !strings.Contains(err.Error(), "no file") {
		t.Errorf("an empty scope: err %v", err)
	}
}

func TestExcludesFlagPrintsThemAndRefusesABadScope(t *testing.T) {
	root, cfg := moduleWith(t, scopedConfig, "internal/server", "askops.go", "noteops.go", "login.go")
	t.Chdir(root)
	var out, errb bytes.Buffer
	if code := run([]string{"-config", cfg, "-excludes", "internal/server"}, &out, &errb); code != 0 ||
		out.String() != "^login\\.go$\n" {
		t.Fatalf("exit %d out %q err %q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := run([]string{"-config", cfg, "-excludes", "internal/store"}, &out, &errb); code != 0 || out.Len() != 0 {
		t.Fatalf("an unscoped package: exit %d out %q", code, out.String())
	}
	if err := os.Remove(filepath.Join(root, "internal/server/noteops.go")); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-config", cfg, "-excludes", "internal/server"}, &out, &errb); code != 2 {
		t.Fatalf("a scope with a missing file: exit %d, want 2", code)
	}
}

func TestTheShippedConfigScopesOnlyPackagesItMutates(t *testing.T) {
	data, err := os.ReadFile("../quality.json")
	if err != nil {
		t.Fatal(err)
	}
	var all struct {
		MutationGo config `json:"mutation_go"`
	}
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	cfg := all.MutationGo
	if cfg.Efficacy != 80 || !cfg.FailOnNoMutants {
		t.Fatalf("the floor moved: efficacy %v, fail_on_no_mutants %v", cfg.Efficacy, cfg.FailOnNoMutants)
	}
	in := map[string]bool{}
	for _, p := range cfg.Packages {
		in[p] = true
	}
	// The handwritten application logic Phase 2 introduced or changed.
	for _, p := range []string{"internal/domain", "internal/store", "internal/asks", "internal/attention", "internal/checkin",
		"internal/cursors", "internal/events", "internal/feed", "internal/notes", "internal/server", "internal/cli"} {
		if !in[p] {
			t.Errorf("%s is not mutated", p)
		}
	}
	for p, files := range cfg.Scopes {
		if !in[p] {
			t.Errorf("scope for %s, which is not a mutated package", p)
		}
		if _, err := excludes(cfg, "../..", p); err != nil {
			t.Errorf("%s: %v (files %v)", p, err, files)
		}
	}
	for _, f := range []string{"askwait.go", "streamops.go"} {
		if !slices.Contains(cfg.Scopes["internal/server"], f) {
			t.Errorf("internal/server scope lacks %s", f)
		}
	}
}

const lanedConfig = `{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,
"packages":["internal/domain","internal/store","internal/asks","internal/server"],
"lanes":{"base":["internal/domain","internal/store"],"logic":["internal/asks"],"server":["internal/server"]}}}`

func laneRun(t *testing.T, config string, args []string, reports map[string]string) (int, string) {
	t.Helper()
	root, cfg := moduleWith(t, config, "internal/server", "a.go")
	for name, content := range reports {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	code := run(append([]string{"-config", cfg, "-dir", root}, args...), &out, &errb)
	return code, out.String() + errb.String()
}

func TestALaneListsAndJudgesOnlyItsOwnPackages(t *testing.T) {
	code, out := laneRun(t, lanedConfig, []string{"-list", "-lane", "base"}, nil)
	if code != 0 || out != "internal/domain\ninternal/store\n" {
		t.Fatalf("base lane list: exit %d %q", code, out)
	}
	if code, out = laneRun(t, lanedConfig, []string{"-list"}, nil); code != 0 || strings.Count(out, "\n") != 4 {
		t.Fatalf("no lane lists every package: exit %d %q", code, out)
	}
	// Only the server report exists: the server lane passes on it alone, the
	// others fail for a missing report, and "all" fails too.
	reports := map[string]string{"internal-server.json": gremlinsReport(9, 1, 0, 0)}
	if code, out = laneRun(t, lanedConfig, []string{"-lane", "server"}, reports); code != 0 {
		t.Fatalf("server lane: exit %d\n%s", code, out)
	}
	if code, out = laneRun(t, lanedConfig, []string{"-lane", "logic"}, reports); code != 1 || !strings.Contains(out, "internal/asks: FAILED, report") {
		t.Fatalf("logic lane without its report: exit %d\n%s", code, out)
	}
	if code, _ = laneRun(t, lanedConfig, nil, reports); code != 1 {
		t.Fatalf("all lanes with three reports missing: exit %d, want 1", code)
	}
	// A lane still judges against the unchanged floor.
	low := map[string]string{"internal-server.json": gremlinsReport(3, 1, 0, 0)}
	if code, out = laneRun(t, lanedConfig, []string{"-lane", "server"}, low); code != 1 || !strings.Contains(out, "efficacy 75.00% is below the floor 80.00%") {
		t.Fatalf("server lane at 75%%: exit %d\n%s", code, out)
	}
}

func TestLanesMustPartitionThePackages(t *testing.T) {
	for name, tc := range map[string]struct{ config, want string }{
		"unknown lane": {lanedConfig, "unknown lane"},
		"left out": {`{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,"packages":["a","b"],"lanes":{"x":["a"]}}}`,
			"b is in no lane"},
		"twice": {`{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,"packages":["a"],"lanes":{"x":["a"],"y":["a"]}}}`,
			"a is in lanes"},
		"not a package": {`{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,"packages":["a"],"lanes":{"x":["a","c"]}}}`,
			"c, which is not in mutation_go.packages"},
		"empty lane": {`{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,"packages":["a"],"lanes":{"x":["a"],"y":[]}}}`,
			"lists no package"},
		"reserved": {`{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,"packages":["a"],"lanes":{"all":["a"]}}}`,
			"reserved"},
	} {
		t.Run(name, func(t *testing.T) {
			args := []string{"-list"}
			if name == "unknown lane" {
				args = []string{"-list", "-lane", "nope"}
			}
			if code, out := laneRun(t, tc.config, args, nil); code != 2 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit %d, want 2 naming %q:\n%s", code, tc.want, out)
			}
		})
	}
}

func TestTheShippedLanesPartitionThePackagesAndEveryLaneStaysWithinTheOldBounds(t *testing.T) {
	data, err := os.ReadFile("../quality.json")
	if err != nil {
		t.Fatal(err)
	}
	var all struct {
		MutationGo config `json:"mutation_go"`
	}
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	cfg := all.MutationGo
	if len(cfg.Lanes) == 0 {
		t.Fatal("the shipped config defines no lanes")
	}
	if err := cfg.checkLanes(); err != nil {
		t.Fatal(err)
	}
	var union []string
	for name := range cfg.Lanes {
		pkgs, err := cfg.lanePackages(name)
		if err != nil {
			t.Fatal(err)
		}
		union = append(union, pkgs...)
	}
	sort.Strings(union)
	want := cfg.units()
	sort.Strings(want)
	if strings.Join(union, ",") != strings.Join(want, ",") {
		t.Fatalf("lanes cover %v, packages are %v", union, want)
	}
}

// partedConfig splits internal/server's scope into two parts, run in two lanes.
const partedConfig = `{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,
"packages":["internal/store","internal/server"],
"scopes":{"internal/server":["a.go","b.go","c.go"]},
"parts":{"internal/server":{"x":["a.go"],"y":["b.go","c.go"]}},
"lanes":{"base":["internal/store"],"sx":["internal/server#x"],"sy":["internal/server#y"]}}}`

func TestAPartIsItsOwnUnitWithItsOwnReportAndJudgement(t *testing.T) {
	if got := reportName("internal/server#x"); got != "internal-server-x.json" {
		t.Fatalf("reportName of a part = %q", got)
	}
	if code, out := laneRun(t, partedConfig, []string{"-list"}, nil); code != 0 ||
		out != "internal/store\ninternal/server#x\ninternal/server#y\n" {
		t.Fatalf("units of all lanes: exit %d %q", code, out)
	}
	if code, out := laneRun(t, partedConfig, []string{"-list", "-lane", "sy"}, nil); code != 0 || out != "internal/server#y\n" {
		t.Fatalf("lane sy: exit %d %q", code, out)
	}
	// A part passing says nothing about its sibling: the other part's report is
	// missing, and a part below the floor fails although the package as a whole
	// would pass.
	ok := map[string]string{"internal-server-x.json": gremlinsReport(9, 1, 0, 0)}
	if code, out := laneRun(t, partedConfig, []string{"-lane", "sx"}, ok); code != 0 {
		t.Fatalf("lane sx: exit %d\n%s", code, out)
	}
	if code, out := laneRun(t, partedConfig, []string{"-lane", "sy"}, ok); code != 1 || !strings.Contains(out, "internal/server#y: FAILED, report") {
		t.Fatalf("lane sy without its report: exit %d\n%s", code, out)
	}
	low := map[string]string{"internal-server-x.json": gremlinsReport(9, 1, 0, 0), "internal-server-y.json": gremlinsReport(3, 1, 0, 0)}
	if code, out := laneRun(t, partedConfig, []string{"-lane", "sy"}, low); code != 1 || !strings.Contains(out, "efficacy 75.00% is below the floor 80.00%") {
		t.Fatalf("part y at 75%%: exit %d\n%s", code, out)
	}
}

func TestAPartExcludesEveryFileOutsideItself(t *testing.T) {
	root, cfgPath := moduleWith(t, partedConfig, "internal/server", "a.go", "b.go", "c.go", "d.go", "a_test.go")
	cfg, _, err := loadLane(cfgPath, "all")
	if err != nil {
		t.Fatal(err)
	}
	for unit, want := range map[string][]string{
		"internal/server#x": {"^b\\.go$", "^c\\.go$", "^d\\.go$"},
		"internal/server#y": {"^a\\.go$", "^d\\.go$"},
	} {
		got, err := excludes(cfg, root, unit)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("excludes(%s) = %v, %v; want %v", unit, got, err, want)
		}
	}
	if _, err := excludes(cfg, root, "internal/server#nope"); err == nil {
		t.Error("a part that does not exist was accepted")
	}
}

func TestPartsMustSplitTheScopeExactly(t *testing.T) {
	const head = `{"mutation_go":{"enabled":true,"efficacy":80,"fail_on_no_mutants":true,"packages":["p","q"],`
	for name, tc := range map[string]struct{ config, want string }{
		"a file in two parts": {head + `"scopes":{"p":["a.go","b.go"]},"parts":{"p":{"x":["a.go","b.go"],"y":["b.go"]}},"lanes":{"l":["p#x","p#y","q"]}}}`,
			"b.go is in parts"},
		"a scoped file in no part": {head + `"scopes":{"p":["a.go","b.go"]},"parts":{"p":{"x":["a.go"]}},"lanes":{"l":["p#x","q"]}}}`,
			"b.go is in no part"},
		"a part file outside the scope": {head + `"scopes":{"p":["a.go"]},"parts":{"p":{"x":["a.go","z.go"]}},"lanes":{"l":["p#x","q"]}}}`,
			"z.go, which is not in the package's scope"},
		"an empty part": {head + `"scopes":{"p":["a.go"]},"parts":{"p":{"x":["a.go"],"y":[]}},"lanes":{"l":["p#x","p#y","q"]}}}`,
			"lists no file"},
		"parts of an unscoped package": {head + `"parts":{"q":{"x":["a.go"]}},"lanes":{"l":["p","q#x"]}}}`,
			"not a scoped package"},
		"a part in no lane": {head + `"scopes":{"p":["a.go","b.go"]},"parts":{"p":{"x":["a.go"],"y":["b.go"]}},"lanes":{"l":["p#x","q"]}}}`,
			"p#y is in no lane"},
		"a part in two lanes": {head + `"scopes":{"p":["a.go","b.go"]},"parts":{"p":{"x":["a.go"],"y":["b.go"]}},"lanes":{"l":["p#x","p#y","q"],"m":["p#x"]}}}`,
			"p#x is in lanes"},
		"the whole package in a lane while it has parts": {head + `"scopes":{"p":["a.go"]},"parts":{"p":{"x":["a.go"]}},"lanes":{"l":["p","p#x","q"]}}}`,
			"p, which is not in mutation_go.packages"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, out := laneRun(t, tc.config, []string{"-list"}, nil); code != 2 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit %d, want 2 naming %q:\n%s", code, tc.want, out)
			}
		})
	}
}

func TestTheShippedPartsSplitTheServerScopeIntoTwoLanes(t *testing.T) {
	data, err := os.ReadFile("../quality.json")
	if err != nil {
		t.Fatal(err)
	}
	var all struct {
		MutationGo config `json:"mutation_go"`
	}
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	cfg := all.MutationGo
	if err := cfg.checkLanes(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Parts["internal/server"]) != 2 {
		t.Fatalf("internal/server parts = %v, want two", cfg.Parts["internal/server"])
	}
	// Every part is judged on its own and is run by a lane of its own.
	lanes := map[string]string{}
	for lane, units := range cfg.Lanes {
		for _, u := range units {
			lanes[u] = lane
		}
	}
	if lanes["internal/server#ops"] == "" || lanes["internal/server#ops"] == lanes["internal/server#streams"] {
		t.Fatalf("the two server parts share a lane or have none: %v", lanes)
	}
	for _, unit := range cfg.units() {
		if _, err := excludes(cfg, "../..", unit); err != nil {
			t.Errorf("%s: %v", unit, err)
		}
	}
}
