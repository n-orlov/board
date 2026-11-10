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
//	mutationgate -config ci/quality.json -unmeasured
//	                                               # application files changed since phase_base
//	                                               # that no unit measures (exit 1 if any)
//	mutationgate -config ci/quality.json -excludes internal/server
//	                                               # one gremlins -E regexp per out-of-scope file
//	                                               # (a part: internal/server#ops)
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
	"os/exec"
	"path"
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
	// Parts splits one scoped package's files into named parts, each of which
	// is its own unit: a lane lists it as "internal/server#ops", gremlins runs
	// the package once per part with every file outside the part excluded, and
	// the part has its own report and its own efficacy judgement against the
	// same floor. It exists because gremlins re-runs a package's whole suite
	// for every mutant, so one lane cannot always hold a large package's
	// mutants inside its 30-minute stage timeout (none is raised). The parts of
	// a package are disjoint and together are exactly its scope, so splitting
	// never leaves a scoped file unmeasured.
	Parts map[string]map[string][]string `json:"parts"`
	// Lanes splits Packages into groups that ci/mutation.sh runs, and the
	// Jenkinsfile runs as separate stages, each under its own timeout: a
	// gremlins run of a package re-runs the package's whole suite for every
	// mutant, and one stage cannot hold them all in 30 minutes. Every package
	// is in exactly one lane (a package with Parts: each of its parts, written
	// "package#part"). No lanes means one lane, "all".
	Lanes map[string][]string `json:"lanes"`
	// PhaseBase is the commit the current phase started from. Every
	// handwritten application file (a non-test, non-generated .go file under
	// internal/ or cmd/) added or changed since it must be measured by some
	// unit: its package is listed and, if scoped, its file is in the scope.
	// -unmeasured lists the ones that are not, and a test of the shipped
	// config requires none, so the scope cannot silently fall behind the
	// phase's code.
	PhaseBase string `json:"phase_base"`
}

// allLane is the lane name that selects every package.
const allLane = "all"

// partSep separates a package from the name of one of its parts in a unit.
const partSep = "#"

// splitUnit splits a unit into its package directory and part ("" for a unit
// that is a whole package).
func splitUnit(unit string) (pkg, part string) {
	pkg, part, _ = strings.Cut(unit, partSep)
	return pkg, part
}

// units returns what gremlins runs and the gate judges, in the configured
// order: a package, or for a package with Parts each of its parts by name.
func (c config) units() []string {
	var out []string
	for _, p := range c.Packages {
		parts, ok := c.Parts[p]
		if !ok {
			out = append(out, p)
			continue
		}
		names := make([]string, 0, len(parts))
		for n := range parts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, p+partSep+n)
		}
	}
	return out
}

// lanePackages returns the units of a lane in the configured order, or an
// error for a lane the config does not define.
func (c config) lanePackages(lane string) ([]string, error) {
	if lane == "" || lane == allLane {
		return c.units(), nil
	}
	in, ok := c.Lanes[lane]
	if !ok {
		return nil, fmt.Errorf("unknown lane %q (lanes: %s)", lane, strings.Join(c.laneNames(), ", "))
	}
	var out []string
	for _, u := range c.units() {
		if slices.Contains(in, u) {
			out = append(out, u)
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
	if err := c.checkParts(); err != nil {
		return err
	}
	if len(c.Lanes) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, name := range c.laneNames() {
		if err := c.checkLane(name, seen); err != nil {
			return err
		}
	}
	for _, u := range c.units() {
		if _, ok := seen[u]; !ok {
			return fmt.Errorf("%s is in no lane, so no stage would measure it", u)
		}
	}
	return nil
}

// checkParts fails a config whose parts would measure less than the scope
// they split, or measure a file twice: a part belongs to a scoped package, and
// the parts of a package are non-empty, disjoint, and together exactly its
// scope.
func (c config) checkParts() error {
	for _, pkg := range sortedKeys(c.Parts) {
		scope, ok := c.Scopes[pkg]
		if !slices.Contains(c.Packages, pkg) || !ok {
			return fmt.Errorf("parts for %s, which is not a scoped package of mutation_go.packages", pkg)
		}
		if err := checkPartsOf(pkg, scope, c.Parts[pkg]); err != nil {
			return err
		}
	}
	return nil
}

// checkPartsOf checks that parts are non-empty, disjoint and exactly scope.
func checkPartsOf(pkg string, scope []string, parts map[string][]string) error {
	owner := map[string]string{}
	for _, name := range sortedKeys(parts) {
		if len(parts[name]) == 0 {
			return fmt.Errorf("%s: part %q lists no file", pkg, name)
		}
		for _, f := range parts[name] {
			if prev, dup := owner[f]; dup {
				return fmt.Errorf("%s: %s is in parts %q and %q, so it would be mutated twice", pkg, f, prev, name)
			}
			if !slices.Contains(scope, f) {
				return fmt.Errorf("%s: part %q lists %s, which is not in the package's scope", pkg, name, f)
			}
			owner[f] = name
		}
	}
	for _, f := range scope {
		if _, ok := owner[f]; !ok {
			return fmt.Errorf("%s: scoped file %s is in no part, so no stage would measure it", pkg, f)
		}
	}
	return nil
}

// sortedKeys returns the keys of m in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkLane checks one lane and records its packages in seen (package -> lane).
func (c config) checkLane(name string, seen map[string]string) error {
	if name == allLane {
		return fmt.Errorf("lane %q is reserved for every package", allLane)
	}
	if len(c.Lanes[name]) == 0 {
		return fmt.Errorf("lane %q lists no package", name)
	}
	units := c.units()
	for _, p := range c.Lanes[name] {
		if !slices.Contains(units, p) {
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

// reportName is the file a unit's report lives in: internal/domain ->
// internal-domain.json, and a part internal/server#ops ->
// internal-server-ops.json. ci/mutation.sh writes the same names.
func reportName(pkg string) string {
	return strings.NewReplacer("/", "-", partSep, "-").Replace(strings.Trim(pkg, "./")) + ".json"
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
func excludes(cfg config, root, unit string) ([]string, error) {
	pkg, part := splitUnit(strings.Trim(unit, "./"))
	scope, ok := cfg.Scopes[pkg]
	if !ok {
		return nil, nil
	}
	if part != "" {
		scope, ok = cfg.Parts[pkg][part]
		if !ok {
			return nil, fmt.Errorf("%s: no part %q", pkg, part)
		}
	}
	present, err := nonTestGoFiles(filepath.Join(root, pkg))
	if err != nil {
		return nil, err
	}
	return outOfScope(pkg, scope, present)
}

// outOfScope returns one anchored regexp per file of present that scope does
// not list. A scoped file that is not in present, or a scope with no file, is
// an error.
func outOfScope(pkg string, scope []string, present map[string]bool) ([]string, error) {
	if len(scope) == 0 {
		return nil, fmt.Errorf("%s: its scope lists no file", pkg)
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
	unmeas := fs.Bool("unmeasured", false, "print each application file changed since phase_base that no unit measures; exit 1 if there is one")
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
	case *unmeas:
		return reportUnmeasured(cfg, stdout, stderr)
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

// appRoots are the module directories that hold the product's application
// code; everything else (ci/, features/, e2e/) is tooling or tests.
var appRoots = []string{"internal", "cmd"}

// generatedRE is Go's marker for a generated file (go help generate).
var generatedRE = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// measures reports whether some unit of the config mutates file, a
// slash-separated path relative to the module root.
func (c config) measures(file string) bool {
	dir, name := path.Split(file)
	dir = strings.TrimSuffix(dir, "/")
	if !slices.Contains(c.Packages, dir) {
		return false
	}
	scope, ok := c.Scopes[dir]
	return !ok || slices.Contains(scope, name)
}

// changedSince lists the files under appRoots that differ between base and
// the working tree and still exist (git diff, deletions left out).
func changedSince(root, base string) ([]string, error) {
	if base == "" {
		return nil, errors.New("mutation_go.phase_base is not set, so the phase's changed files cannot be listed")
	}
	args := append([]string{"diff", "--name-only", "--no-renames", "--diff-filter=d", base, "--"}, appRoots...)
	cmd := exec.Command("git", args...) //nolint:gosec // G204: git diff against the base commit the repository's own config names
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %w", base, err)
	}
	return strings.Fields(string(out)), nil
}

// unmeasured returns, sorted, each handwritten application file changed
// since the config's phase_base (a non-test .go file that is not generated)
// that no unit measures.
func unmeasured(cfg config, root string) ([]string, error) {
	changed, err := changedSince(root, cfg.PhaseBase)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range changed {
		needs, err := needsUnit(cfg, root, f)
		if err != nil {
			return nil, err
		}
		if needs {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

// needsUnit reports whether a changed file is handwritten application code
// (a non-test, non-generated .go file) that no unit of cfg measures.
func needsUnit(cfg config, root, f string) (bool, error) {
	if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") || cfg.measures(f) {
		return false, nil
	}
	generated, err := isGenerated(filepath.Join(root, filepath.FromSlash(f)))
	return !generated, err
}

// isGenerated reports whether the Go file at path carries the generated-code
// marker.
func isGenerated(path string) (bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: a path git diff listed inside the repository
	if err != nil {
		return false, err
	}
	return generatedRE.Match(data), nil
}

// reportUnmeasured prints the unmeasured files of the working directory's
// module and returns the exit code: 0 none, 1 some, 2 an error.
func reportUnmeasured(cfg config, stdout, stderr io.Writer) int {
	root, err := os.Getwd()
	if err == nil {
		var files []string
		if files, err = unmeasured(cfg, root); err == nil {
			for _, f := range files {
				_, _ = fmt.Fprintf(stdout, "%s: changed since %s but no mutation unit measures it\n", f, cfg.PhaseBase)
			}
			if len(files) > 0 {
				return 1
			}
			return 0
		}
	}
	_, _ = fmt.Fprintln(stderr, "mutationgate:", err)
	return 2
}

func printLines(w io.Writer, lines []string) {
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
