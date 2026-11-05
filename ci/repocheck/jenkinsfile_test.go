// jenkinsfile_test.go probes the Jenkinsfile (board/ci). Plain text
// matching; the file is small and declarative. The stage helpers are deck's
// (deck's ci/repocheck/jenkinsfile_test.go); deck's own Jenkinsfile probes
// join them here with task 011.
package repocheck

import (
	"regexp"
	"strings"
	"testing"
)

var stageRE = regexp.MustCompile(`stage\('([^']+)'\)\s*\{`)

// jenkinsStages returns the stage names in the order they appear.
func jenkinsStages(jf string) []string {
	var names []string
	for _, m := range stageRE.FindAllStringSubmatch(jf, -1) {
		names = append(names, m[1])
	}
	return names
}

// jenkinsStage returns the body of stage(name), up to its matching brace.
func jenkinsStage(jf, name string) (string, bool) {
	for _, loc := range stageRE.FindAllStringSubmatchIndex(jf, -1) {
		if jf[loc[2]:loc[3]] != name {
			continue
		}
		depth := 0
		for i := loc[1] - 1; i < len(jf); i++ {
			switch jf[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return jf[loc[1]:i], true
				}
			}
		}
	}
	return "", false
}

func stageBefore(jf, first, second string) bool {
	fi, si := -1, -1
	for i, n := range jenkinsStages(jf) {
		if n == first && fi < 0 {
			fi = i
		}
		if n == second && si < 0 {
			si = i
		}
	}
	return fi >= 0 && si >= 0 && fi < si
}

// TestJenkinsfileMutationStageRunsTheMutationGate: the Jenkinsfile's
// Mutation stage exists, comes last (after Quality and Stability, R13's
// order), runs on every main build and in the extended lane, runs
// `ci/run.sh ci/mutation.sh` as its command, and lets a red gate fail the
// build.
func TestJenkinsfileMutationStageRunsTheMutationGate(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	stages := jenkinsStages(jf)
	if len(stages) == 0 || stages[len(stages)-1] != "Mutation" {
		t.Fatalf("stages = %q, want Mutation last", stages)
	}
	for _, before := range []string{"Quality", "Stability"} {
		if !stageBefore(jf, before, "Mutation") {
			t.Errorf("stage %s does not run before Mutation: %q", before, stages)
		}
	}
	body, _ := jenkinsStage(jf, "Mutation")
	lines := jenkinsfileLines(body)
	if !anyLineContains(lines, "sh 'ci/run.sh ci/mutation.sh'") {
		t.Errorf("the Mutation stage does not run `ci/run.sh ci/mutation.sh`:\n%s", body)
	}
	for _, want := range []string{"anyOf", "branch 'main'", "environment name: 'CI_NIGHTLY', value: 'true'"} {
		if !anyLineContains(lines, want) {
			t.Errorf("the Mutation stage's when {} lacks %q (every main build and the extended lane):\n%s", want, body)
		}
	}
	if !anyLineContains(lines, "catchError(buildResult: 'FAILURE'") {
		t.Errorf("a red mutation gate does not fail the build:\n%s", body)
	}
	if !anyLineContains(jenkinsfileLines(jf), "ci-results/mutation/*.json") {
		t.Error("the gremlins reports in ci-results/mutation/ are not archived")
	}
}

// TestJenkinsStageHelpers: the helpers read stage names in order, and a
// stage body runs to its own closing brace, nested blocks included.
func TestJenkinsStageHelpers(t *testing.T) {
	jf := "stages {\n stage('A') { steps { sh 'a' } }\n stage('B') {\n when { branch 'main' }\n steps { sh 'b' }\n }\n}\n"
	if got := strings.Join(jenkinsStages(jf), ","); got != "A,B" {
		t.Fatalf("jenkinsStages = %q, want A,B", got)
	}
	body, ok := jenkinsStage(jf, "B")
	if !ok || !strings.Contains(body, "branch 'main'") || !strings.Contains(body, "sh 'b'") || strings.Contains(body, "sh 'a'") {
		t.Fatalf("jenkinsStage(B) = %q, %v", body, ok)
	}
	if _, ok := jenkinsStage(jf, "C"); ok {
		t.Fatal("jenkinsStage found a stage that does not exist")
	}
	if !stageBefore(jf, "A", "B") || stageBefore(jf, "B", "A") || stageBefore(jf, "A", "C") {
		t.Fatal("stageBefore misorders A and B, or accepts a missing stage")
	}
}
