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
	"strings"
)

// config is the "mutation_go" section of ci/quality.json.
type config struct {
	Enabled         bool     `json:"enabled"`
	Efficacy        float64  `json:"efficacy"`
	Packages        []string `json:"packages"`
	FailOnNoMutants bool     `json:"fail_on_no_mutants"`
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

// evaluate gates every configured package and returns the report text and
// whether all of them passed.
func evaluate(cfg config, dir string) (string, bool) {
	if len(cfg.Packages) == 0 {
		return "mutation-go gate: FAILED, no package is configured\n", false
	}
	var b strings.Builder
	allOK := true
	for _, pkg := range cfg.Packages {
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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *cfgPath == "" {
		_, _ = fmt.Fprintln(stderr, "mutationgate: -config is required")
		return 2
	}
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mutationgate:", err)
		return 2
	}
	if *list {
		for _, p := range cfg.Packages {
			_, _ = fmt.Fprintln(stdout, p)
		}
		return 0
	}
	text, ok := evaluate(cfg, *dir)
	_, _ = fmt.Fprint(stdout, text)
	if !ok {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
