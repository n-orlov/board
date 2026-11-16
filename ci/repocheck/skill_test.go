// skill_test.go checks skill/SKILL.md, the SPEC §9 identity template and
// INSTALL-AGENT.md against the CLI they tell an agent to run (R13): the file's
// shape, every `vh …` span against cli.Commands(), and every VH_/BOARD_ name
// against the variable table or the installer's own list.
package repocheck

import (
	"regexp"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/cli"
	"github.com/n-orlov/village-hall/internal/envvar"
)

// maxSkillLines is the length cap on skill/SKILL.md.
const maxSkillLines = 15

// installerVariables is R8's list: the names install.sh and INSTALL-AGENT.md
// read that are not in the product's variable table.
var installerVariables = []string{
	"VH_VERSION", "VH_INSTALL_DIR", "VH_REQUIRE_IMMUTABLE", "VH_REPO", "VH_API_URL", "VH_DOWNLOAD_URL",
}

var variableName = regexp.MustCompile(`\b(?:VH|BOARD)_[A-Z][A-Z0-9_]*`)

// unknownVariables returns each VH_ or BOARD_ name in text that is neither in
// the variable table (as either spelling) nor in the installer list.
func unknownVariables(text string) []string {
	var bad []string
	seen := map[string]bool{}
	for _, name := range variableName.FindAllString(text, -1) {
		if seen[name] || envvar.Known(name) || contains(installerVariables, name) {
			continue
		}
		seen[name] = true
		bad = append(bad, name)
	}
	return bad
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// skillShapeProblems checks the frontmatter and the length of a skill file.
func skillShapeProblems(text string) []string {
	var problems []string
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > maxSkillLines {
		problems = append(problems, "runs to more lines than the cap")
	}
	if len(lines) < 3 || lines[0] != "---" {
		return append(problems, "has no frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return append(problems, "frontmatter is not closed")
	}
	var name, description string
	for _, l := range lines[1:end] {
		if v, ok := strings.CutPrefix(l, "name:"); ok {
			name = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(l, "description:"); ok {
			description = strings.TrimSpace(v)
		}
	}
	if name != "village-hall" {
		problems = append(problems, "frontmatter name is not village-hall")
	}
	if description == "" {
		problems = append(problems, "frontmatter has no description")
	}
	return problems
}

// referenceProblems is every unknown command, flag and variable in text.
func referenceProblems(text string) []string {
	problems := cli.UnknownReferences(text, cli.Commands())
	for _, name := range unknownVariables(text) {
		problems = append(problems, "unknown variable "+name)
	}
	return problems
}

// fencedBlock returns the body of the first ```markdown block in text at or
// after the line that starts with marker.
func fencedBlock(t *testing.T, text, marker string) string {
	t.Helper()
	_, rest, ok := strings.Cut(text, marker)
	if !ok {
		t.Fatalf("no %q", marker)
	}
	_, rest, ok = strings.Cut(rest, "```markdown\n")
	if !ok {
		t.Fatalf("no markdown block after %q", marker)
	}
	body, _, ok := strings.Cut(rest, "```\n")
	if !ok {
		t.Fatalf("markdown block after %q is not closed", marker)
	}
	return body
}

func identityTemplate(t *testing.T) string {
	t.Helper()
	spec := readRepoFile(t, "SPEC.md")
	_, afterNine, ok := strings.Cut(spec, "## 9. Agent identity")
	section, _, ok2 := strings.Cut(afterNine, "## 10. Agent skill")
	if !ok || !ok2 {
		t.Fatal("SPEC.md has no §9 ending at §10")
	}
	return fencedBlock(t, section, "Template for the operator's instructions")
}

func TestSkillFileHasTheFrontmatterAndFitsTheLineCap(t *testing.T) {
	if problems := skillShapeProblems(readRepoFile(t, "skill/SKILL.md")); len(problems) != 0 {
		t.Fatalf("skill/SKILL.md: %q", problems)
	}
}

func TestSkillShapeRulesFailOnEachDefect(t *testing.T) {
	good := "---\nname: village-hall\ndescription: a board\n---\nbody\n"
	if got := skillShapeProblems(good); len(got) != 0 {
		t.Fatalf("good file flagged %q", got)
	}
	cases := []struct{ name, text, want string }{
		{"no frontmatter", "body\nmore\nlines\n", "no frontmatter"},
		{"unclosed", "---\nname: village-hall\ndescription: x\nbody\n", "not closed"},
		{"wrong name", "---\nname: board\ndescription: x\n---\nbody\n", "name is not village-hall"},
		{"no name", "---\ndescription: x\n---\nbody\n", "name is not village-hall"},
		{"no description", "---\nname: village-hall\ndescription:\n---\nbody\n", "no description"},
		{"too long", good + strings.Repeat("line\n", maxSkillLines), "more lines"},
	}
	for _, c := range cases {
		if got := strings.Join(skillShapeProblems(c.text), "; "); !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q do not contain %q", c.name, got, c.want)
		}
	}
	atCap := good + strings.Repeat("line\n", maxSkillLines-5)
	if got := skillShapeProblems(atCap); len(got) != 0 {
		t.Errorf("a file of exactly %d lines flagged %q", maxSkillLines, got)
	}
}

func TestSkillFileTellsTheAgentWhatToDo(t *testing.T) {
	text := readRepoFile(t, "skill/SKILL.md")
	for _, want := range []string{"`vh help agent`", "operator's", "INSTALL-AGENT.md", "`vh` is missing"} {
		if !strings.Contains(text, want) {
			t.Errorf("skill/SKILL.md does not say %q", want)
		}
	}
}

func TestSkillFileIsTheCopySpecSection10Shows(t *testing.T) {
	spec := readRepoFile(t, "SPEC.md")
	_, section10, ok := strings.Cut(spec, "## 10. Agent skill")
	if !ok {
		t.Fatal("SPEC.md has no §10")
	}
	shown := fencedBlock(t, section10, "`skill/SKILL.md` (generic")
	if shown != readRepoFile(t, "skill/SKILL.md") {
		t.Fatal("SPEC §10 shows a different skill/SKILL.md than the file")
	}
}

func TestSpecIdentityTemplateUsesVHNamesAndKeepsTheOldOnesAccepted(t *testing.T) {
	block := identityTemplate(t)
	for _, v := range []envvar.Var{envvar.URL, envvar.APIKey, envvar.Actor, envvar.WayBack} {
		if !strings.Contains(block, v.Name()) {
			t.Errorf("the §9 template does not use %s", v.Name())
		}
		if !strings.Contains(block, v.Alias()) {
			t.Errorf("the §9 template does not name the accepted %s", v.Alias())
		}
	}
	if !strings.Contains(block, "still accepted") {
		t.Error("the §9 template does not state that the BOARD_ names are still accepted")
	}
}

func TestEveryCommandAndVariableAnAgentDocumentNamesExists(t *testing.T) {
	documents := map[string]string{
		"skill/SKILL.md":   readRepoFile(t, "skill/SKILL.md"),
		"SPEC §9 template": identityTemplate(t),
		"INSTALL-AGENT.md": readRepoFile(t, "INSTALL-AGENT.md"),
	}
	for name, text := range documents {
		if !strings.Contains(text, "`vh ") {
			t.Errorf("%s names no `vh …` command: the check below would pass on nothing", name)
		}
		if problems := referenceProblems(text); len(problems) != 0 {
			t.Errorf("%s: %q", name, problems)
		}
	}
}

func TestReferenceCheckFailsOnAPlantedUnknownCommandAndVariable(t *testing.T) {
	good := "Run `vh help agent`, then `vh whoami`. Set VH_URL, BOARD_ACTOR and VH_REPO."
	if got := referenceProblems(good); len(got) != 0 {
		t.Fatalf("good text flagged %q", got)
	}
	if got := strings.Join(referenceProblems(good+" Then `vh frobnicate`."), "; "); !strings.Contains(got, `unknown command "vh frobnicate"`) {
		t.Errorf("a planted unknown command gave %q", got)
	}
	if got := strings.Join(referenceProblems(good+" Also `vh whoami --nonesuch`."), "; "); !strings.Contains(got, "unknown flag --nonesuch") {
		t.Errorf("a planted unknown flag gave %q", got)
	}
	for _, planted := range []string{"VH_NO_SUCH_THING", "BOARD_NO_SUCH_THING", "VH_URLS"} {
		if got := strings.Join(referenceProblems(good+" Set "+planted+"."), "; "); !strings.Contains(got, "unknown variable "+planted) {
			t.Errorf("a planted %s gave %q", planted, got)
		}
	}
}
