package repocheck

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ci/mutation-plan.sh plans incremental mutation: from the reference build's
// reports and commit, and ci/mutationgate -stale's verdict, it picks the units
// that run in each full-mode group, copies the reports of the rest, and says
// full when the reference is unusable or a shared input changed. These tests
// run it in a scratch repository with a stub go standing in for
// ci/mutationgate: units p/a and p/b (lane g1), p/c (g2) and p/d (base).

const stubPlanGo = `#!/bin/sh
# go run ./ci/mutationgate -config <config> -stale <ref> | -list [-lane <list>]
shift 4
case "$1" in
-stale)
    if [ -n "${STUB_FULL:-}" ]; then
        echo "mutationgate: full: $STUB_FULL" >&2
        echo "exit status 3" >&2
        exit 1
    fi
    [ -z "${STUB_BROKEN:-}" ] || { echo "mutationgate: boom" >&2; exit 1; }
    for u in ${STUB_STALE:-}; do echo "$u"; done ;;
-list)
    if [ "${2:-}" != -lane ]; then
        printf '%s\n' p/a p/b p/c p/d
        exit 0
    fi
    for l in $(echo "$3" | tr , ' '); do
        case $l in
        g1) printf '%s\n' p/a p/b ;;
        g2) echo p/c ;;
        base) echo p/d ;;
        *) echo "$l" ;;
        esac
    done ;;
esac
`

type planCase struct {
	env      []string // STUB_STALE, STUB_FULL, ...
	webEdit  bool     // web/ changes after the reference commit
	reports  []string // the reference's reports; nil means every unit's
	verdict  string   // the reference's unit-verdict.txt; "" writes none
	commit   string   // the reference's commit.txt; "ref" is the reference commit
	groups   []string
	sunday   bool
	wantCode int
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil { //nolint:gosec // G703: a path under the test's temp dirs
		t.Fatal(err)
	}
}

// runPlan checks the script's exit status and returns its plan file (empty
// when it wrote none) and the names in the reused outdir.
func runPlan(t *testing.T, c planCase) (string, []string) {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(root, "ci", "mutation-plan.sh"))
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	writeFile(t, filepath.Join(repo, "ci", "mutation-plan.sh"), string(script))
	writeFile(t, filepath.Join(repo, "web", "app.ts"), "1\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", "reference")
	ref := git(t, repo, "rev-parse", "HEAD")
	if c.webEdit {
		writeFile(t, filepath.Join(repo, "web", "app.ts"), "2\n")
		git(t, repo, "commit", "-qam", "web")
	}

	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "go"), stubPlanGo)
	day := "3"
	if c.sunday {
		day = "7"
	}
	writeFile(t, filepath.Join(bin, "date"), "#!/bin/sh\necho "+day+"\n")
	for _, f := range []string{"go", "date"} {
		if err := os.Chmod(filepath.Join(bin, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	raw := t.TempDir()
	reports := c.reports
	if reports == nil {
		reports = []string{"p-a", "p-b", "p-c", "p-d"}
	}
	for _, r := range reports {
		writeFile(t, filepath.Join(raw, "ci-results", "mutation", r+".json"), "{}")
	}
	if c.verdict != "" {
		writeFile(t, filepath.Join(raw, "ci-results", "mutation", "unit-verdict.txt"), c.verdict)
		writeFile(t, filepath.Join(raw, "ci-results", "mutation", "stryker", "mutation.json"), "{}")
	}
	if c.commit != "" {
		writeFile(t, filepath.Join(raw, "ci-results", "commit.txt"), strings.Replace(c.commit, "ref", ref, 1)+"\n")
	}

	work := t.TempDir()
	reused := filepath.Join(work, "reused")
	plan := filepath.Join(work, "plan.txt")
	args := append([]string{"ci/mutation-plan.sh", raw, reused, "ci/quality.json", plan}, c.groups...)
	cmd := exec.Command("sh", args...)
	cmd.Dir = repo
	cmd.Env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + t.TempDir()}, c.env...)
	out, err := cmd.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("ci/mutation-plan.sh: %v\n%s", err, out)
	}
	if code != c.wantCode {
		t.Fatalf("exit %d, want %d\n%s", code, c.wantCode, out)
	}
	text, _ := os.ReadFile(plan)
	var names []string
	_ = filepath.WalkDir(reused, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(reused, p)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	return string(text), names
}

var planGroups = []string{"one=g1", "two=g2", "base=base"}

const greenVerdict = "mutation-go: passed\nstryker: passed\n"

func TestMutationPlanRunsOnlyTheStaleUnits(t *testing.T) {
	for name, c := range map[string]struct {
		pc       planCase
		plan     string
		reusedTo string
	}{
		"one stale unit, Stryker reused": {
			planCase{env: []string{"STUB_STALE=p/b"}, verdict: greenVerdict, commit: "ref", groups: planGroups},
			"mode=incremental\nref=REF\nrun=1\nreused=3\nstryker=reuse\ngroup.one=p/b\ngroup.two=\ngroup.base=\n",
			"p-a.json p-c.json p-d.json stryker/mutation.json unit-verdict.txt",
		},
		"web changed: Stryker and base run": {
			planCase{env: []string{"STUB_STALE=p/b"}, webEdit: true, verdict: greenVerdict, commit: "ref", groups: planGroups},
			"mode=incremental\nref=REF\nrun=2\nreused=2\nstryker=run\ngroup.one=p/b\ngroup.two=\ngroup.base=base\n",
			"p-a.json p-c.json",
		},
		"the reference's Stryker failed": {
			planCase{verdict: "mutation-go: passed\nstryker: failed\n", commit: "ref", groups: planGroups},
			"mode=incremental\nref=REF\nrun=1\nreused=3\nstryker=run\ngroup.one=\ngroup.two=\ngroup.base=base\n",
			"p-a.json p-b.json p-c.json",
		},
		"a unit the reference never reported": {
			planCase{reports: []string{"p-a", "p-b", "p-d"}, verdict: greenVerdict, commit: "ref", groups: planGroups},
			"mode=incremental\nref=REF\nrun=1\nreused=3\nstryker=reuse\ngroup.one=\ngroup.two=p/c\ngroup.base=\n",
			"p-a.json p-b.json p-d.json stryker/mutation.json unit-verdict.txt",
		},
		"nothing stale": {
			planCase{verdict: greenVerdict, commit: "ref", groups: planGroups},
			"mode=incremental\nref=REF\nrun=0\nreused=4\nstryker=reuse\ngroup.one=\ngroup.two=\ngroup.base=\n",
			"p-a.json p-b.json p-c.json p-d.json stryker/mutation.json unit-verdict.txt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, reused := runPlan(t, c.pc)
			ref := ""
			for _, line := range strings.Split(plan, "\n") {
				if v, ok := strings.CutPrefix(line, "ref="); ok {
					ref = v
				}
			}
			if want := strings.Replace(c.plan, "REF", ref, 1); len(ref) != 40 || plan != want {
				t.Errorf("plan %q, want %q", plan, want)
			}
			if got := strings.Join(reused, " "); got != c.reusedTo {
				t.Errorf("reused %q, want %q", got, c.reusedTo)
			}
		})
	}
}

func TestMutationPlanSaysFullWhenNothingCanBeReused(t *testing.T) {
	for reason, c := range map[string]planCase{
		"Sunday's weekly full run":                     {env: []string{"BOARD_MUTATION_WEEKLY_FULL=yes"}, sunday: true, verdict: greenVerdict, commit: "ref", groups: planGroups},
		"the reference build left no mutation results": {commit: "ref", groups: planGroups},
		"the reference build left no commit":           {verdict: greenVerdict, groups: planGroups},
		"is not in this checkout":                      {verdict: greenVerdict, commit: "0123456789abcdef0123456789abcdef01234567", groups: planGroups},
		"go.sum changed":                               {env: []string{"STUB_FULL=go.sum changed"}, verdict: greenVerdict, commit: "ref", groups: planGroups},
	} {
		t.Run(reason, func(t *testing.T) {
			plan, _ := runPlan(t, c)
			if !strings.HasPrefix(plan, "mode=full\nreason=") || !strings.Contains(plan, reason) {
				t.Errorf("plan %q, want full because %s", plan, reason)
			}
		})
	}
	// Weekly full runs only on Sundays.
	if plan, _ := runPlan(t, planCase{env: []string{"BOARD_MUTATION_WEEKLY_FULL=yes"}, verdict: greenVerdict, commit: "ref", groups: planGroups}); !strings.HasPrefix(plan, "mode=incremental") {
		t.Errorf("a weekday's nightly is not incremental: %q", plan)
	}
}

func TestMutationPlanFailsWithoutAPlan(t *testing.T) {
	for name, c := range map[string]planCase{
		"no group holds base": {verdict: greenVerdict, commit: "ref", groups: []string{"one=g1", "two=g2"}, wantCode: 1},
		"-stale broke":        {env: []string{"STUB_BROKEN=yes"}, verdict: greenVerdict, commit: "ref", groups: planGroups, wantCode: 1},
		"no groups":           {verdict: greenVerdict, commit: "ref", wantCode: 2},
	} {
		t.Run(name, func(t *testing.T) {
			runPlan(t, c)
		})
	}
}
