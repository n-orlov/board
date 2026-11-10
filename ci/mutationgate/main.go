// ci/mutationgate: the Go mutation gate (SPEC 15.4). It reads the
// "mutation_go" section of ci/quality.json and one gremlins JSON report per
// configured package, and fails when
//
//   - a package's report is missing or unreadable,
//   - a package generated no mutant (fail_on_no_mutants: a gate that
//     measured nothing has not passed), or
//   - a package's efficacy is below the configured floor.
//
// Efficacy is gremlins' own definition: KILLED / (KILLED + LIVED). Timed-out,
// not-viable and not-covered mutants are reported but sit outside it. A
// package whose mutants are all not covered has no efficacy and fails.
//
// The counts are taken from the per-mutation statuses in the report, not
// from its summary fields, which omit timed-out and not-covered mutants.
//
//	mutationgate -config ci/quality.json -dir ci-results/mutation
//	mutationgate -config ci/quality.json -list     # the packages, one per line
//	mutationgate -config ci/quality.json -list -lane logic
//	                                               # one lane's packages
//	mutationgate -config ci/quality.json -dir d -lane logic
//	                                               # judge one lane's reports
//	mutationgate -config ci/quality.json -excludes internal/server
//	                                               # one gremlins -E regexp per out-of-scope file
//
// Exit codes: 0 pass, 1 gate failed, 2 usage or config error.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// config is the "mutation_go" section of ci/quality.json.
type config struct {
	Enabled         bool     `json:"enabled"`
	Efficacy        float64  `json:"efficacy"`
	Packages        []string `json:"packages"`
	FailOnNoMutants bool     `json:"fail_on_no_mutants"`
	// Scopes narrows a package to the handwritten files listed for it (base
	// names in the package directory): every other non-test .go file of the
	// package is excluded from the gremlins run. A package with no entry is
	// mutated whole. It is how a large package that holds both older code and
	// code a phase introduced or changed (internal/server, internal/cli) is
	// measured over the phase's own files without a per-test bound raised.
	Scopes map[string][]string `json:"scopes"`
	// Lanes splits Packages into groups that ci/mutation.sh runs, and the
	// Jenkinsfile runs as separate stages, each under its own timeout: a
	// gremlins run of a package re-runs the package's whole suite for every
	// mutant, and one stage cannot hold them all in 30 minutes. Every package
	// is in exactly one lane. No lanes means one lane, "all".
	Lanes map[string][]string `json:"lanes"`
}

// allLane is the lane name that selects every package.
const allLane = "all"

// lanePackages returns the packages of a lane in the configured order, or an
// error for a lane the config does not define.
func (c config) lanePackages(lane string) ([]string, error) {
	if lane == "" || lane == allLane {
		return c.Packages, nil
	}
	in, ok := c.Lanes[lane]
	if !ok {
		return nil, fmt.Errorf("unknown lane %q (lanes: %s)", lane, strings.Join(c.laneNames(), ", "))
	}
	var out []string
	for _, p := range c.Packages {
		if slices.Contains(in, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (c config) laneNames() []string {
	names := make([]string, 0, len(c.Lanes))
	for n := range c.Lanes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// checkLanes fails a config whose lanes do not partition its packages: a
// package left out of every lane would never be measured by any stage, one in
// two lanes would be measured twice, and a lane naming a package the gate does
// not list measures something the gate never judges.
func (c config) checkLanes() error {
	if len(c.Lanes) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, name := range c.laneNames() {
		if err := c.checkLane(name, seen); err != nil {
			return err
		}
	}
	for _, p := range c.Packages {
		if _, ok := seen[p]; !ok {
			return fmt.Errorf("%s is in no lane, so no stage would measure it", p)
		}
	}
	return nil
}

// checkLane checks one lane and records its packages in seen (package -> lane).
func (c config) checkLane(name string, seen map[string]string) error {
	if name == allLane {
		return fmt.Errorf("lane %q is reserved for every package", allLane)
	}
	if len(c.Lanes[name]) == 0 {
		return fmt.Errorf("lane %q lists no package", name)
	}
	for _, p := range c.Lanes[name] {
		if !slices.Contains(c.Packages, p) {
			return fmt.Errorf("lane %q lists %s, which is not in mutation_go.packages", name, p)
		}
		if prev, dup := seen[p]; dup {
			return fmt.Errorf("%s is in lanes %q and %q", p, prev, name)
		}
		seen[p] = name
	}
	return nil
}

// report is the part of a gremlins JSON report the gate reads.
type report struct {
	Files []struct {
		Mutations []struct {
			Status string `json:"status"`
		} `json:"mutations"`
	} `json:"files"`
}

// counts is a package's mutants by outcome.
type counts struct {
	Killed, Lived, TimedOut, NotViable, NotCovered, Other int
}

// total is every mutant gremlins generated that compiles: everything but
// the not-viable ones.
func (c counts) total() int {
	return c.Killed + c.Lived + c.TimedOut + c.NotCovered + c.Other
}

// efficacy is KILLED over KILLED + LIVED, as a percentage; ok is false when
// no mutant was judged.
func (c counts) efficacy() (pct float64, ok bool) {
	judged := c.Killed + c.Lived
	if judged == 0 {
		return 0, false
	}
	return 100 * float64(c.Killed) / float64(judged), true
}

// add counts one mutation status.
func (c *counts) add(status string) {
	switch status {
	case "KILLED":
		c.Killed++
	case "LIVED":
		c.Lived++
	case "TIMED OUT":
		c.TimedOut++
	case "NOT VIABLE":
		c.NotViable++
	case "NOT COVERED":
		c.NotCovered++
	default:
		c.Other++
	}
}

// tally counts the mutation statuses of a gremlins report.
func tally(data []byte) (counts, error) {
	var r report
	if err := json.Unmarshal(data, &r); err != nil {
		return counts{}, err
	}
	var c counts
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			c.add(m.Status)
		}
	}
	return c, nil
}

// reportName is the file a package's report lives in: internal/domain ->
// internal-domain.json. ci/mutation.sh writes the same names.
func reportName(pkg string) string {
	return strings.ReplaceAll(strings.Trim(pkg, "./"), "/", "-") + ".json"
}

// judge is one package's verdict: a failure message, or "" when it passes.
func judge(cfg config, c counts) string {
	if c.total() == 0 {
		if cfg.FailOnNoMutants {
			return "FAILED: gremlins generated no mutant, so nothing was measured"
		}
		return ""
	}
	pct, ok := c.efficacy()
	if !ok {
		return "FAILED: no mutant was judged (every mutant is not covered or timed out), so there is no efficacy"
	}
	if pct < cfg.Efficacy {
		return fmt.Sprintf("FAILED: efficacy %.2f%% is below the floor %.2f%%", pct, cfg.Efficacy)
	}
	return ""
}

// evaluatePackages gates the given packages (a lane, or all of them) and
// returns the report text and whether all of them passed.
func evaluatePackages(cfg config, dir string, pkgs []string) (string, bool) {
	if len(pkgs) == 0 {
		return "mutation-go gate: FAILED, no package is configured\n", false
	}
	var b strings.Builder
	allOK := true
	for _, pkg := range pkgs {
		line, ok := evaluatePackage(cfg, dir, pkg)
		b.WriteString(line)
		allOK = allOK && ok
	}
	fmt.Fprintf(&b, "mutation-go gate: efficacy floor %.2f%%, zero mutants fail=%v\n", cfg.Efficacy, cfg.FailOnNoMutants)
	return b.String(), allOK
}

func evaluatePackage(cfg config, dir, pkg string) (string, bool) {
	path := filepath.Join(dir, reportName(pkg))
	data, err := os.ReadFile(path) //nolint:gosec // G304: the report path is the gate's own directory argument plus a name derived from the configured package
	if err != nil {
		return fmt.Sprintf("%s: FAILED, report %s unreadable: %v\nwhat to do: ci/mutation.sh writes it\n", pkg, path, err), false
	}
	c, err := tally(data)
	if err != nil {
		return fmt.Sprintf("%s: FAILED, report %s is not a gremlins report: %v\n", pkg, path, err), false
	}
	pct, _ := c.efficacy()
	summary := fmt.Sprintf("%s: %d mutants (killed %d, lived %d, timed out %d, not covered %d, not viable %d), efficacy %.2f%%",
		pkg, c.total(), c.Killed, c.Lived, c.TimedOut, c.NotCovered, c.NotViable, pct)
	if msg := judge(cfg, c); msg != "" {
		return summary + "\n" + pkg + ": " + msg + "\n", false
	}
	return summary + ": ok\n", true
}

// excludes returns one anchored regexp per non-test .go file of the package
// that its scope leaves out, for gremlins' --exclude-files (matched against
// the file's path relative to the package directory, which is what gremlins
// matches). root is the module root; a package with no scope has no
// exclusion. A scoped file that does not exist is an error: a scope that
// names nothing real would silently measure less than it claims.
func excludes(cfg config, root, pkg string) ([]string, error) {
	pkg = strings.Trim(pkg, "./")
	scope, ok := cfg.Scopes[pkg]
	if !ok {
		return nil, nil
	}
	if len(scope) == 0 {
		return nil, fmt.Errorf("%s: its scope lists no file", pkg)
	}
	present, err := nonTestGoFiles(filepath.Join(root, pkg))
	if err != nil {
		return nil, err
	}
	for _, f := range scope {
		if !present[f] {
			return nil, fmt.Errorf("%s: scoped file %s is not a non-test .go file of the package", pkg, f)
		}
		delete(present, f)
	}
	out := make([]string, 0, len(present))
	for n := range present {
		out = append(out, "^"+regexp.QuoteMeta(n)+"$")
	}
	sort.Strings(out)
	return out, nil
}

// nonTestGoFiles is the set of a directory's non-test .go file names.
func nonTestGoFiles(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			present[n] = true
		}
	}
	return present, nil
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the config path is the tool's own command-line argument
	if err != nil {
		return config{}, err
	}
	var all struct {
		MutationGo config `json:"mutation_go"`
	}
	if err := json.Unmarshal(data, &all); err != nil {
		return config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if !all.MutationGo.Enabled {
		return config{}, errors.New("mutation_go is not enabled in " + path + ": the gate is never skipped")
	}
	return all.MutationGo, nil
}

// run is main's testable core and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mutationgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "", "path to ci/quality.json (required)")
	dir := fs.String("dir", "ci-results/mutation", "directory holding one gremlins JSON report per package")
	list := fs.Bool("list", false, "print the configured packages and exit")
	lane := fs.String("lane", allLane, "judge or list only this lane of mutation_go.lanes")
	excl := fs.String("excludes", "", "print, one per line, the gremlins --exclude-files regexps that keep `package` to its configured scope, and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, pkgs, err := loadLane(*cfgPath, *lane)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mutationgate:", err)
		return 2
	}
	switch {
	case *list:
		printLines(stdout, pkgs)
	case *excl != "":
		lines, err := excludesFromCwd(cfg, *excl)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "mutationgate:", err)
			return 2
		}
		printLines(stdout, lines)
	default:
		text, ok := evaluatePackages(cfg, *dir, pkgs)
		_, _ = fmt.Fprint(stdout, text)
		if !ok {
			return 1
		}
	}
	return 0
}

// loadLane reads the config, checks its lanes and returns the packages of one
// lane.
func loadLane(cfgPath, lane string) (config, []string, error) {
	if cfgPath == "" {
		return config{}, nil, errors.New("-config is required")
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return config{}, nil, err
	}
	if err := cfg.checkLanes(); err != nil {
		return config{}, nil, err
	}
	pkgs, err := cfg.lanePackages(lane)
	return cfg, pkgs, err
}

// excludesFromCwd is excludes with the working directory as the module root.
func excludesFromCwd(cfg config, pkg string) ([]string, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return excludes(cfg, root, pkg)
}

func printLines(w io.Writer, lines []string) {
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
