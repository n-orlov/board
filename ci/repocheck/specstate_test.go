// specstate_test.go holds the current-state passages of SPEC.md to the product they describe (R14): the identity
// model (several operators may coexist) and the web shell's route table, its navigation order, its default route and
// its badges. A statement about an earlier phase may keep its old words only when it says it is an earlier phase.
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

func TestSpecNowIsDescribedAsTheDefaultRouteWithTwoBadges(t *testing.T) {
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
	for _, want := range []string{
		"Now 3 items need attention 2 open tasks have a live lease",
		"`#/` and `#/attention` land on `#/now`",
		"carries **two badges**",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("SPEC.md does not say %q", want)
		}
	}
}

var historical = regexp.MustCompile(`(?i)phase|replaced|was the|named attention|attention in`)

func TestSpecAttentionViewAndSingleOperatorAreOnlyEverHistorical(t *testing.T) {
	for n, line := range strings.Split(readRepoFile(t, "SPEC.md"), "\n") {
		for _, old := range []string{"Attention view", "Attention page", "one operator", "One operator"} {
			if strings.Contains(line, old) && !historical.MatchString(line) {
				t.Errorf("SPEC.md:%d uses %q in the present tense: %.160s", n+1, old, line)
			}
		}
	}
}
