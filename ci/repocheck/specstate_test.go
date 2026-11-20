// specstate_test.go holds the current-state passages of SPEC.md to the product they describe (R14): the identity
// model (several operators may coexist) and the web shell's route table, its navigation order, its default route and
// its badges, each shown or hidden on its own. README.md's introduction is held to the same identity and view. A
// statement about an earlier phase may keep its old words only when it says it is an earlier phase.
package repocheck

import (
	"regexp"
	"strings"
	"testing"
)

var routeEntry = regexp.MustCompile(`(?s)\{\s*id: '([a-z]+)',\s*hash: '[^']*',(.*?)\n  \}`)
var routeLabel = regexp.MustCompile(`\blabel: '([^']+)'`)
var routeParent = regexp.MustCompile(`\bparent: '`)

// routeTable returns the ids of every entry of web/src/lib/routes.ts in table order and the labels of those that
// have a navigation entry (no `parent`).
func routeTable(t *testing.T) (ids, navLabels []string) {
	t.Helper()
	src := readRepoFile(t, "web/src/lib/routes.ts")
	for _, m := range routeEntry.FindAllStringSubmatch(src, -1) {
		ids = append(ids, m[1])
		if !routeParent.MatchString(m[2]) {
			navLabels = append(navLabels, routeLabel.FindStringSubmatch(m[2])[1])
		}
	}
	if len(ids) < 8 || len(navLabels) < 8 {
		t.Fatalf("routes.ts parsed to ids %q and labels %q", ids, navLabels)
	}
	return ids, navLabels
}

func TestSpecNavigationOrderAndRouteIdsAreTheRouteTable(t *testing.T) {
	spec := readRepoFile(t, "SPEC.md")
	ids, labels := routeTable(t)
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "`" + id + "`"
	}
	for _, want := range []string{
		"The navigation order is " + strings.Join(labels, ", "),
		"the table's ids are, in order, " + strings.Join(quoted, ", "),
		"its first entry, " + labels[0] + ", the default view",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("SPEC.md does not say %q", want)
		}
	}
}

func TestSpecNowIsDescribedAsTheDefaultRoute(t *testing.T) {
	spec := readRepoFile(t, "SPEC.md")
	for _, stale := range []string{
		"first entry, Attention",
		"The navigation order is Attention",
		`"Attention 3 items need attention"`,
		"lands on Attention",
	} {
		if strings.Contains(spec, stale) {
			t.Errorf("SPEC.md still says %q", stale)
		}
	}
	if want := "`#/` and `#/attention` land on `#/now`"; !strings.Contains(spec, want) {
		t.Errorf("SPEC.md does not say %q", want)
	}
}

// Nav.svelte draws the red and the green badge under conditions of their own, so the Now entry shows both, either one
// or neither. SPEC.md must say so and must never claim the entry always carries both.
var ownBadgeCondition = regexp.MustCompile(`\{#if entry\.(badge|liveBadge) !== ''\}`)
var alwaysBothBadges = regexp.MustCompile(`(?i)(two badges\*{0,2},? never one|(?:^|[^n] )carries \*{0,2}two (?:small count )?badges|always (?:carries|shows) (?:two|both) badges)`)

func TestSpecNowBadgesAreEachShownOrHiddenOnTheirOwn(t *testing.T) {
	if got := ownBadgeCondition.FindAllStringSubmatch(readRepoFile(t, "web/src/Nav.svelte"), -1); len(got) != 2 {
		t.Fatalf("Nav.svelte no longer draws each badge under its own condition (%q); revisit SPEC.md's badge passages", got)
	}
	spec := readRepoFile(t, "SPEC.md")
	for n, line := range strings.Split(spec, "\n") {
		if m := alwaysBothBadges.FindString(line); m != "" {
			t.Errorf("SPEC.md:%d says the Now entry always carries both badges (%q), but each is hidden at zero on its own", n+1, m)
		}
	}
	for _, want := range []string{
		"can carry **two badges, each shown or hidden on its own**",
		"each shown or hidden on its own (both, either one, or neither is on screen)",
		`"Now 3 items need attention 2 open tasks have a live lease"`,
		`with only the red one it is "Now 2 items need attention"`,
		`with only the green one "Now 3 open tasks have a live lease"`,
		`with none it is "Now"`,
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("SPEC.md does not say %q", want)
		}
	}
}

var historical = regexp.MustCompile(`(?i)phase|replaced|was the|named attention|attention in`)

func TestSpecAttentionViewAndSingleOperatorAreOnlyEverHistorical(t *testing.T) {
	for _, file := range []string{"SPEC.md", "README.md"} {
		for n, line := range strings.Split(readRepoFile(t, file), "\n") {
			for _, old := range []string{"Attention view", "Attention page", "attention view", "one operator", "One operator"} {
				if strings.Contains(line, old) && !historical.MatchString(line) {
					t.Errorf("%s:%d uses %q in the present tense: %.160s", file, n+1, old, line)
				}
			}
		}
	}
}
