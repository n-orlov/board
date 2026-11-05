// Command legcompare fails when a scenario is green on one database leg of the
// feature matrix and red on another (SPEC 15.2: the same scenario must give the
// same answer on SQLite and on Postgres).
//
//	go run ./ci/legcompare sqlite=<junit.xml> postgres=<junit.xml> [<leg>=<junit.xml>...]
//
// Each input is the merged JUnit of one leg (ci/suite.sh's
// junit-merged/junit-features-<leg>.xml). A test is keyed by its classname and
// name, and its outcome on a leg is red when any attempt of it carries a
// <failure> or <error> (a test that failed once and then passed was folded by
// ci/junitflaky into one passing case, so it is green), skipped when it carries
// <skipped>, and green otherwise. The aggregate Go test TestFeatures is left
// out: it is red on a leg exactly when one of that leg's scenarios is, which
// would report every divergence twice.
//
// A scenario red on every leg is not a divergence (the leg's own failure
// already fails the suite), and a scenario only some legs run (one tagged for a
// single database) has nothing to disagree with. The exit status is 1 when any
// scenario diverges, 2 on a usage or input error, and 0 otherwise.
package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type problem struct{}

type testCase struct {
	Name      string     `xml:"name,attr"`
	ClassName string     `xml:"classname,attr"`
	Failure   []*problem `xml:"failure"`
	Error     []*problem `xml:"error"`
	Skipped   []*problem `xml:"skipped"`
}

type testSuite struct {
	Cases  []testCase  `xml:"testcase"`
	Suites []testSuite `xml:"testsuite"`
}

type testSuites struct {
	Suites []testSuite `xml:"testsuite"`
	Cases  []testCase  `xml:"testcase"`
}

const (
	green   = "green"
	red     = "red"
	skipped = "skipped"
)

// aggregate is the Go test that wraps every scenario of a leg.
const aggregate = "TestFeatures"

func outcomeOf(c testCase) string {
	switch {
	case len(c.Failure)+len(c.Error) > 0:
		return red
	case len(c.Skipped) > 0:
		return skipped
	}
	return green
}

// merge keeps the worst outcome seen for one test: red beats skipped beats green.
func merge(have, got string) string {
	if have == red || got == red {
		return red
	}
	if have == skipped || got == skipped {
		return skipped
	}
	return green
}

func collect(suites []testSuite, cases []testCase, into map[string]string) {
	for _, c := range cases {
		if c.Name == aggregate {
			continue
		}
		key := c.ClassName + " " + c.Name
		if have, ok := into[key]; ok {
			into[key] = merge(have, outcomeOf(c))
		} else {
			into[key] = outcomeOf(c)
		}
	}
	for _, s := range suites {
		collect(s.Suites, s.Cases, into)
	}
}

// readLeg returns the outcome of every test in one JUnit file.
func readLeg(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the JUnit path is the tool's own command-line argument
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return parseLeg(f)
}

func parseLeg(r io.Reader) (map[string]string, error) {
	var doc testSuites
	if err := xml.NewDecoder(r).Decode(&doc); err != nil { //nolint:gosec // G709: the input is a JUnit file this suite wrote itself, and only attribute values are read
		return nil, err
	}
	out := map[string]string{}
	collect(doc.Suites, doc.Cases, out)
	return out, nil
}

// leg is one named result set.
type leg struct {
	name     string
	outcomes map[string]string
}

// divergences lists, sorted, every test that is green on one leg and red on
// another, with the outcome each leg gave it.
func divergences(legs []leg) []string {
	keys := map[string]bool{}
	for _, l := range legs {
		for k := range l.outcomes {
			keys[k] = true
		}
	}
	var lines []string
	for k := range keys {
		if line := divergence(k, legs); line != "" {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return lines
}

func divergence(key string, legs []leg) string {
	var seen []string
	hasGreen, hasRed := false, false
	for _, l := range legs {
		o, ok := l.outcomes[key]
		if !ok {
			continue
		}
		hasGreen = hasGreen || o == green
		hasRed = hasRed || o == red
		seen = append(seen, l.name+"="+o)
	}
	if !hasGreen || !hasRed {
		return ""
	}
	return key + ": " + strings.Join(seen, " ")
}

func parseArgs(args []string) ([]leg, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: legcompare <leg>=<junit.xml> <leg>=<junit.xml> [...]")
	}
	var legs []leg
	for _, a := range args {
		name, path, ok := strings.Cut(a, "=")
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("argument %q is not <leg>=<junit.xml>", a)
		}
		outcomes, err := readLeg(path)
		if err != nil {
			return nil, fmt.Errorf("leg %s: %w", name, err)
		}
		legs = append(legs, leg{name: name, outcomes: outcomes})
	}
	return legs, nil
}

// run compares the legs named by args and returns the process exit status.
func run(args []string, stdout, stderr io.Writer) int {
	legs, err := parseArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "legcompare:", err)
		return 2
	}
	lines := divergences(legs)
	for _, l := range lines {
		_, _ = fmt.Fprintln(stdout, "DIVERGENT", l)
	}
	if len(lines) > 0 {
		_, _ = fmt.Fprintf(stderr, "legcompare: %d scenario(s) differ between database legs\n", len(lines))
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
