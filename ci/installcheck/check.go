package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// facts are the six dimensions the two files must agree on. Lists are
// sorted and de-duplicated so the comparison is a set comparison.
type facts struct {
	asset    string
	oses     []string
	arches   []string
	checksum string
	dir      string
	vars     []string
}

// Check parses install.sh and INSTALL-AGENT.md and returns one line per
// dimension on which they disagree. The error is non-nil when a file lacks
// a fact the check needs, which is a failure of the check itself.
func Check(script, guide string) ([]string, error) {
	fromScript, err := parseScript(script)
	if err != nil {
		return nil, fmt.Errorf("install.sh: %w", err)
	}
	fromGuide, runtime, err := parseGuide(guide)
	if err != nil {
		return nil, fmt.Errorf("INSTALL-AGENT.md: %w", err)
	}
	var out []string
	add := func(dim, a, b string) {
		if a != b {
			out = append(out, fmt.Sprintf("%s: install.sh has %s, INSTALL-AGENT.md has %s", dim, a, b))
		}
	}
	add("asset name pattern", fromScript.asset, fromGuide.asset)
	add("OS list", list(fromScript.oses), list(fromGuide.oses))
	add("arch list", list(fromScript.arches), list(fromGuide.arches))
	add("checksum file name", fromScript.checksum, fromGuide.checksum)
	add("default install directory", fromScript.dir, fromGuide.dir)
	add("environment variable names", list(fromScript.vars), list(fromGuide.vars))
	// Every variable the guide names anywhere is an installer variable or a
	// runtime one it declares; a stray name is a disagreement too.
	known := append(slices.Clone(fromGuide.vars), runtime...)
	for _, v := range varNames(guide) {
		if !slices.Contains(known, v) {
			out = append(out, fmt.Sprintf("environment variable names: INSTALL-AGENT.md mentions %s, which is in neither of its variable lists", v))
		}
	}
	return out, nil
}

func list(items []string) string { return "[" + strings.Join(items, ", ") + "]" }

func sortedSet(items []string) []string {
	out := slices.Clone(items)
	slices.Sort(out)
	return slices.Compact(out)
}

var varRe = regexp.MustCompile(`\bVH_[A-Z][A-Z0-9_]*\b`)

func varNames(text string) []string { return sortedSet(varRe.FindAllString(text, -1)) }

var (
	assetRe    = regexp.MustCompile(`(?m)^asset="([^"\n]+)"`)
	checksumRe = regexp.MustCompile(`(?m)^checksums=(\S+)$`)
	dirRe      = regexp.MustCompile(`(?m)^dir="\$\{VH_[A-Z_]+:-([^}"\n]+)\}"`)
	osCaseRe   = regexp.MustCompile(`(?s)case "\$os" in\n(.*?)\nesac`)
	archCaseRe = regexp.MustCompile(`(?s)case "\$arch" in\n(.*?)\nesac`)
	archSetRe  = regexp.MustCompile(`\barch=([a-z0-9_]+)`)
)

func parseScript(script string) (facts, error) {
	var f facts
	asset := assetRe.FindStringSubmatch(script)
	checksum := checksumRe.FindStringSubmatch(script)
	dir := dirRe.FindStringSubmatch(script)
	osCase := osCaseRe.FindStringSubmatch(script)
	archCase := archCaseRe.FindStringSubmatch(script)
	for _, need := range []struct {
		name string
		m    []string
	}{
		{"asset=", asset}, {"checksums=", checksum}, {"dir=", dir}, {`case "$os"`, osCase}, {`case "$arch"`, archCase},
	} {
		if need.m == nil {
			return f, fmt.Errorf("no %s line found", need.name)
		}
	}
	f.asset = strings.NewReplacer("${os}", "<os>", "${arch}", "<arch>").Replace(asset[1])
	f.checksum = checksum[1]
	f.dir = strings.Replace(dir[1], "$HOME", "~", 1)
	f.oses = casePatterns(osCase[1])
	f.arches = archOutputs(archCase[1])
	f.vars = varNames(script)
	return f, nil
}

// casePatterns lists the literal patterns of the arms of a shell case body
// that accept a value (the `*)` fallback is the refusal and is skipped).
func casePatterns(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		head, _, ok := strings.Cut(line, ")")
		if !ok {
			continue
		}
		for _, p := range strings.Split(head, "|") {
			if p = strings.TrimSpace(p); p != "*" {
				out = append(out, p)
			}
		}
	}
	return sortedSet(out)
}

// archOutputs lists the values the arch case assigns to `arch`: the names
// the assets carry, whatever uname spelling selected them.
func archOutputs(body string) []string {
	var out []string
	for _, m := range archSetRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return sortedSet(out)
}

var (
	factRe    = regexp.MustCompile("(?m)^- (Asset|OS|Arch|Checksums|Directory|Installer variables|Runtime variables): (.+)$")
	tickedRe  = regexp.MustCompile("`([^`]+)`")
	factNames = []string{"Asset", "OS", "Arch", "Checksums", "Directory", "Installer variables", "Runtime variables"}
)

// parseGuide reads the shared-facts list of INSTALL-AGENT.md: one line per
// fact, `- <Label>: ` followed by the values in backticks.
func parseGuide(guide string) (f facts, runtime []string, err error) {
	got, err := collectFacts(guide)
	if err != nil {
		return f, nil, err
	}
	f = facts{
		asset:    got["Asset"][0],
		oses:     sortedSet(got["OS"]),
		arches:   sortedSet(got["Arch"]),
		checksum: got["Checksums"][0],
		dir:      got["Directory"][0],
		vars:     sortedSet(got["Installer variables"]),
	}
	return f, sortedSet(got["Runtime variables"]), nil
}

// collectFacts gathers the fact lines and checks that every fact is there,
// once, with the right number of values.
func collectFacts(guide string) (map[string][]string, error) {
	got := map[string][]string{}
	for _, m := range factRe.FindAllStringSubmatch(guide, -1) {
		if _, dup := got[m[1]]; dup {
			return nil, fmt.Errorf("the %q fact is listed twice", m[1])
		}
		got[m[1]] = tickedValues(m[2])
	}
	for _, name := range factNames {
		if len(got[name]) == 0 {
			return nil, fmt.Errorf("no %q fact (a line `- %s: ` with its values in backticks)", name, name)
		}
	}
	for _, name := range []string{"Asset", "Checksums", "Directory"} {
		if len(got[name]) != 1 {
			return nil, fmt.Errorf("the %q fact must have exactly one value", name)
		}
	}
	return got, nil
}

func tickedValues(text string) []string {
	var items []string
	for _, t := range tickedRe.FindAllStringSubmatch(text, -1) {
		items = append(items, t[1])
	}
	return items
}
