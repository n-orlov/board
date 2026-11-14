// jenkinsfile_test.go probes the Jenkinsfile (board/ci) and Jenkinsfile.release
// (board/release), carried from deck's ci/repocheck/jenkinsfile_test.go and
// adapted to the distributed pipeline: the three phases Core, Stability and
// Mutation run in order on the one AMD agent label, the nightly lane is
// scheduled on main, mutation runs in a diff or a full mode only when asked,
// the quality gates and reports stay wired in, only a fully green main build
// leaves a release candidate, and the release job is a placeholder that
// validates against that candidate and publishes nothing. Each property is a
// pure function of the file text (a *Problems helper) that its test feeds the
// committed Jenkinsfile and copies of it with the property removed or altered,
// so the test fails if the property is lost. Plain text matching; both files
// are small and declarative.
package repocheck

import (
	"encoding/json"
	"regexp"
	"strconv"
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

// topStageRE matches the stages directly under `stages {` (four spaces of
// indent): Core, Stability and Mutation. Quality and the units are nested.
var topStageRE = regexp.MustCompile(`(?m)^    stage\('([^']+)'\)`)

// topStages returns the pipeline's top-level stage names in order.
func topStages(jf string) []string {
	var names []string
	for _, m := range topStageRE.FindAllStringSubmatch(jf, -1) {
		names = append(names, m[1])
	}
	return names
}

var unitNameRE = regexp.MustCompile(`units << \[name: '([^']+)'`)

// coreUnits returns the units the Core stage declares, in order.
func coreUnits(coreBody string) []string {
	var names []string
	for _, m := range unitNameRE.FindAllStringSubmatch(coreBody, -1) {
		names = append(names, m[1])
	}
	return names
}

// replaceOnce returns jf with the first old replaced by new, and fails the
// test when old is not there: a doctored copy that equals the original would
// prove nothing.
func replaceOnce(t *testing.T, jf, old, repl string) string {
	t.Helper()
	if !strings.Contains(jf, old) {
		t.Fatalf("the Jenkinsfile no longer contains %q, so a case doctoring it checks nothing", old)
	}
	return strings.Replace(jf, old, repl, 1)
}

// expectProblems checks a Jenkinsfile property check from both sides: the
// committed file has no problem, and each doctored copy (the property removed
// or altered) is reported.
func expectProblems(t *testing.T, good string, check func(string) []string, doctored map[string]string) {
	t.Helper()
	for _, p := range check(good) {
		t.Errorf("the committed Jenkinsfile: %s", p)
	}
	for name, text := range doctored {
		if text == good {
			t.Errorf("%s: the doctoring changed nothing", name)
			continue
		}
		if len(check(text)) == 0 {
			t.Errorf("%s was not caught", name)
		}
	}
}

// phaseProblems: the three phases Core, Stability and Mutation in that order;
// Core's units are Lint, Suite, Image and Allure smoke with Quality after
// Suite and Image; Stability needs EXTENDED or the nightly timer; Mutation is
// gated on CI_MUTATION alone (EXTENDED on its own runs no mutation); a phase
// runs only while the build has not failed; each unit runs its command.
func phaseProblems(jf string) []string {
	var problems []string
	if got := strings.Join(topStages(jf), ","); got != "Core,Stability,Mutation" {
		problems = append(problems, "top-level stages are "+got+", want Core,Stability,Mutation")
	}
	core, _ := jenkinsStage(jf, "Core")
	if got := strings.Join(coreUnits(core), ","); got != "Lint,Suite,Image,Allure smoke" {
		problems = append(problems, "Core units are "+got+", want Lint,Suite,Image,Allure smoke")
	}
	if !strings.Contains(core, "branches['Quality'] = {") || !strings.Contains(core, "stage('Quality') {") {
		problems = append(problems, "Core does not run Quality as a branch of its own")
	}
	if !strings.Contains(core, "waitUntil(initialRecurrencePeriod: 15000) { signals['Suite'] == true && signals['Image'] == true }") {
		problems = append(problems, "Quality does not wait for Suite and Image")
	}
	for _, want := range []string{
		"ci/run.sh ci/lint.sh",
		"ci/run.sh ci/suite.sh",
		`ci/image-build.sh "$BOARD_CI_OUT/board-image.tar" && ci/image-check.sh "$BOARD_CI_OUT"`,
		"-run '^TestAllureFormatterWritesResultsForAFixtureRun$' ./features/",
		"ci/allure-smoke.sh allure-smoke/fixture allure-smoke/report",
		"ci/run.sh ci/quality.sh",
	} {
		if !strings.Contains(core, want) {
			problems = append(problems, "Core lacks the command "+want)
		}
	}
	stability, _ := jenkinsStage(jf, "Stability")
	if !strings.Contains(stability, "expression { env.CI_NIGHTLY == 'true' && currentBuild.currentResult != 'FAILURE' }") {
		problems = append(problems, "Stability is not gated on EXTENDED or the nightly timer and an earlier phase that did not fail")
	}
	mutation, _ := jenkinsStage(jf, "Mutation")
	problems = append(problems, mutationGateProblems(mutation)...)
	if !strings.Contains(stability, "[1, 2, 3].each { i ->") || strings.Count(stability, "ci/stability.sh 1") != 1 {
		problems = append(problems, "Stability is not three units, each running ci/stability.sh 1")
	}
	return problems
}

// TestJenkinsfileStagesRunInOrder: the three phases of SPEC 15.3 exist and run
// in order, the Core units are the ones SPEC 15.3 lists, and each runs its
// command, so removing one fails here.
func TestJenkinsfileStagesRunInOrder(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, phaseProblems, map[string]string{
		"Stability before Core":           replaceOnce(t, jf, "    stage('Core') {", "    stage('Stability') {\n      steps { echo 'x' }\n    }\n    stage('Core') {"),
		"Mutation before Stability":       replaceOnce(t, replaceOnce(t, jf, "    stage('Stability') {", "    stage('Early') {"), "    stage('Mutation') {", "    stage('Stability') {"),
		"a fourth phase":                  replaceOnce(t, jf, "    stage('Mutation') {", "    stage('Extra') {\n      steps { echo 'x' }\n    }\n    stage('Mutation') {"),
		"no Lint unit":                    replaceOnce(t, jf, "units << [name: 'Lint'", "units << [name: 'Tidy'"),
		"no Allure smoke unit":            replaceOnce(t, jf, "units << [name: 'Allure smoke'", "units << [name: 'Smoke'"),
		"no Image unit":                   replaceOnce(t, jf, "units << [name: 'Image'", "units << [name: 'Picture'"),
		"Quality not waiting for Image":   replaceOnce(t, jf, " && signals['Image'] == true", ""),
		"Quality not waiting for Suite":   replaceOnce(t, jf, "signals['Suite'] == true && ", ""),
		"Lint not running ci/lint.sh":     replaceOnce(t, jf, "ci/run.sh ci/lint.sh", "ci/run.sh true"),
		"Suite not running ci/suite.sh":   replaceOnce(t, jf, "ci/run.sh ci/suite.sh", "ci/run.sh true"),
		"Image not checked":               replaceOnce(t, jf, ` && ci/image-check.sh "$BOARD_CI_OUT"`, ""),
		"smoke not running the fixture":   replaceOnce(t, jf, "TestAllureFormatterWritesResultsForAFixtureRun", "TestNothing"),
		"Stability on every build":        replaceOnce(t, jf, "expression { env.CI_NIGHTLY == 'true' && currentBuild", "expression { currentBuild"),
		"Stability after a failed Core":   replaceOnce(t, jf, "env.CI_NIGHTLY == 'true' && currentBuild.currentResult != 'FAILURE'", "env.CI_NIGHTLY == 'true'"),
		"Mutation on every build":         replaceOnce(t, jf, "expression { env.CI_MUTATION == 'true' && currentBuild", "expression { currentBuild"),
		"Mutation after a failed phase":   replaceOnce(t, jf, "env.CI_MUTATION == 'true' && currentBuild.currentResult != 'FAILURE'", "env.CI_MUTATION == 'true'"),
		"Mutation on EXTENDED alone":      replaceOnce(t, jf, "expression { env.CI_MUTATION == 'true'", "expression { env.CI_NIGHTLY == 'true'"),
		"Mutation on a push of main":      replaceOnce(t, jf, "    stage('Mutation') {\n      when {", "    stage('Mutation') {\n      when {\n        branch 'main'"),
		"two stability units":             replaceOnce(t, jf, "[1, 2, 3].each { i ->", "[1, 2].each { i ->"),
		"stability running three at once": replaceOnce(t, jf, "ci/stability.sh 1", "ci/stability.sh 3"),
	})
	script := readRepoFile(t, "ci", "jenkins-image.sh")
	for _, v := range []string{"BOARD_CI_CACHE_VOLUME", "BOARD_CI_NPM_VOLUME"} {
		if !strings.Contains(script, `fix_volume "$`+v+`"`) {
			t.Errorf("ci/jenkins-image.sh does not make $%s usable by the agent's uid", v)
		}
	}
	if !strings.Contains(script, "docker build -t board-ci:local -f ci/Dockerfile ci") {
		t.Error("ci/jenkins-image.sh does not build board-ci:local from ci/Dockerfile")
	}
	if !strings.Contains(jf, "ci/jenkins-image.sh") {
		t.Error("the Jenkinsfile does not build board-ci:local before a unit runs")
	}
}

var nodeCallRE = regexp.MustCompile(`\bnode\(([^)]*)\)`)

// shapeProblems: the library, one agent label for every unit (the AMD
// constant, which resolves to agent int21h-ws; no arm64 or other label),
// serial builds, a clean workspace, the three booleanParams, the 480-minute
// pipeline timeout, and the per-attempt cleanup of sibling containers by exact
// ID.
func shapeProblems(jf string) []string {
	var problems []string
	lib := strings.Index(jf, "@Library('homelab') _")
	if lib < 0 || lib > strings.Index(jf, "\npipeline {") {
		problems = append(problems, "the library is not loaded before the pipeline")
	}
	lines := jenkinsfileLines(jf)
	if !anyLineContains(lines, "@Field final String AMD = 'board-test && amd64 && docker'") {
		problems = append(problems, "the AMD label constant is not 'board-test && amd64 && docker'")
	}
	if !anyLineContains(lines, "agent none") {
		problems = append(problems, "the pipeline is not `agent none`: every unit must pick its node itself")
	}
	nodes := nodeCallRE.FindAllStringSubmatch(strings.Join(lines, "\n"), -1)
	if len(nodes) < 3 {
		problems = append(problems, "fewer than three node() calls (a unit, the report, the notification)")
	}
	for _, m := range nodes {
		if m[1] != "AMD" {
			problems = append(problems, "node("+m[1]+") does not use the AMD label constant")
		}
	}
	for _, l := range lines {
		if strings.Contains(l, "arm64") || strings.Contains(l, "agent {") || strings.Contains(l, "label '") {
			problems = append(problems, "line "+l+" names another agent or label")
		}
	}
	if !anyLineContains(lines, "def a = [n: attempts.size() + 1, label: AMD,") {
		problems = append(problems, "an attempt does not record the AMD label")
	}
	for _, want := range []string{
		"disableConcurrentBuilds()",
		"timeout(time: 480, unit: 'MINUTES')",
		"git clean -ffdxq",
		"booleanParam(name: 'EXTENDED', defaultValue: false",
		"booleanParam(name: 'MUTATION', defaultValue: false",
		"booleanParam(name: 'MUTATION_FULL', defaultValue: false",
		// ci/run.sh mounts both cache volumes; the agent's uid owns neither
		// until ci/jenkins-image.sh chowns them.
		"BOARD_CI_CACHE_VOLUME = 'board-go-cache-jenkins'",
		"BOARD_CI_NPM_VOLUME = 'board-npm-cache-jenkins'",
		// Each attempt's containers carry the attempt id and are removed by exact ID.
		`String runId = "jenkins-${env.BUILD_NUMBER}-${slug(u.name)}-${a.n}"`,
		`withEnv(["RALPHD_RUN_ID=${runId}"`,
		`ids=\$(docker ps -aq --filter label=ralphd.run=${runId})`,
		`docker rm -f \$ids`,
	} {
		if !anyLineContains(lines, want) {
			problems = append(problems, "lacks "+want)
		}
	}
	if n := strings.Count(jf, "booleanParam("); n != 3 {
		problems = append(problems, "declares "+strconv.Itoa(n)+" booleanParams, want EXTENDED, MUTATION and MUTATION_FULL")
	}
	if i, j := strings.Index(jf, "} finally {"), strings.Index(jf, "docker rm -f"); i < 0 || j < i {
		problems = append(problems, "the container cleanup does not run in a finally")
	}
	return problems
}

// TestJenkinsfileDeclaresTheJobShape: the library, the one agent label every
// unit uses, serial builds, a clean workspace, the three parameters, the
// pipeline timeout, and the per-attempt cleanup of sibling containers.
func TestJenkinsfileDeclaresTheJobShape(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, shapeProblems, map[string]string{
		"no library":                   replaceOnce(t, jf, "@Library('homelab') _", ""),
		"an arm64 unit":                replaceOnce(t, jf, "        node(AMD) {\n          a.node", "        node('board-test && arm64') {\n          a.node"),
		"a literal label in a unit":    replaceOnce(t, jf, "            node(AMD) {\n              sh 'rm -rf", "            node('linux') {\n              sh 'rm -rf"),
		"the notification elsewhere":   replaceOnce(t, jf, "            node(AMD) {\n              notifyTelegram", "            node('other') {\n              notifyTelegram"),
		"another AMD constant":         replaceOnce(t, jf, "'board-test && amd64 && docker'", "'board-test && arm64 && docker'"),
		"a pipeline-level agent":       replaceOnce(t, jf, "  agent none", "  agent { label 'linux' }"),
		"concurrent builds":            replaceOnce(t, jf, "    disableConcurrentBuilds()\n", ""),
		"a shorter pipeline timeout":   replaceOnce(t, jf, "timeout(time: 480, unit: 'MINUTES')", "timeout(time: 120, unit: 'MINUTES')"),
		"no clean workspace":           replaceOnce(t, jf, "sh 'git clean -ffdxq'", "sh 'true'"),
		"no EXTENDED parameter":        replaceOnce(t, jf, "booleanParam(name: 'EXTENDED'", "booleanParam(name: 'WIDER'"),
		"no MUTATION parameter":        replaceOnce(t, jf, "booleanParam(name: 'MUTATION',", "booleanParam(name: 'MUTATE',"),
		"no MUTATION_FULL parameter":   replaceOnce(t, jf, "booleanParam(name: 'MUTATION_FULL'", "booleanParam(name: 'MUTATE_ALL'"),
		"a fourth parameter":           replaceOnce(t, jf, "  parameters {", "  parameters {\n    booleanParam(name: 'MORE', defaultValue: false, description: 'x')"),
		"an attempt id nobody reads":   replaceOnce(t, jf, `withEnv(["RALPHD_RUN_ID=${runId}"`, `withEnv(["RALPHD_RUN=${runId}"`),
		"cleanup of every container":   replaceOnce(t, jf, "docker ps -aq --filter label=ralphd.run=${runId}", "docker ps -aq"),
		"cleanup that removes nothing": replaceOnce(t, jf, `docker rm -f \$ids`, `echo \$ids`),
		"cleanup outside the finally":  replaceOnce(t, jf, "} finally {", "} catch (Throwable never) {"),
	})
}

// nightlyProblems: main is built nightly at 03:17 and nowhere else has a
// timer, and a nightly (or EXTENDED) build adds -race and fresh vulnerability
// databases to Core and runs the Stability phase.
func nightlyProblems(jf string) []string {
	var problems []string
	for _, want := range []string{
		`cron(env.BRANCH_NAME == 'main' ? '17 3 * * *' : '')`,
		"currentBuild.getBuildCauses('hudson.triggers.TimerTrigger$TimerTriggerCause')",
		"boolean nightly = params.EXTENDED || timer",
		"env.CI_NIGHTLY = nightly ? 'true' : 'false'",
		"env.BOARD_CI_GO_EXTRA_FLAGS = nightly ? '-race' : ''",
		"if (env.CI_NIGHTLY == 'true') {",
		"rm -rf /go-cache/trivy/db /go-cache/trivy/java-db /go-cache/xdg-cache/govulncheck",
	} {
		if !strings.Contains(jf, want) {
			problems = append(problems, "lacks "+want)
		}
	}
	if n := strings.Count(jf, "cron("); n != 1 {
		problems = append(problems, "has "+strconv.Itoa(n)+" cron() triggers, want one")
	}
	return problems
}

// TestJenkinsfileNightlyLane: main is built nightly at 03:17 (and no other
// branch has a timer), and a nightly or EXTENDED build adds -race, drops the
// vulnerability databases and runs the Stability phase.
func TestJenkinsfileNightlyLane(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, nightlyProblems, map[string]string{
		"a timer on every branch":             replaceOnce(t, jf, "cron(env.BRANCH_NAME == 'main' ? '17 3 * * *' : '')", "cron('17 3 * * *')"),
		"a timer on another branch":           replaceOnce(t, jf, "env.BRANCH_NAME == 'main' ? '17 3 * * *'", "env.BRANCH_NAME == 'dev' ? '17 3 * * *'"),
		"another time":                        replaceOnce(t, jf, "'17 3 * * *'", "'17 4 * * *'"),
		"a second timer":                      replaceOnce(t, jf, "  triggers {", "  triggers {\n    cron('5 5 * * *')"),
		"a nightly decided by the timer only": replaceOnce(t, jf, "params.EXTENDED || timer", "timer"),
		"no -race":                            replaceOnce(t, jf, "nightly ? '-race' : ''", "''"),
		"cached vulnerability databases":      replaceOnce(t, jf, "rm -rf /go-cache/trivy/db", "rm -rf /go-cache/trivy/none"),
	})
}

// qualityProblems: the Quality unit runs ci/quality.sh over the results with
// the mutation rows deferred to the Mutation phase, needs Suite's and Image's
// stashes, and lets a red gate fail the build.
func qualityProblems(jf string) []string {
	var problems []string
	core, _ := jenkinsStage(jf, "Core")
	i := strings.Index(core, "stage('Quality') {")
	if i < 0 {
		return []string{"Core has no Quality stage"}
	}
	quality := core[i:]
	for _, want := range []string{
		`ci/run.sh ci/quality.sh "$BOARD_CI_OUT" ci/quality.json --mutation-deferred`,
		"needs: ['res-suite', 'res-image', 'image']",
		"catchError(buildResult: 'FAILURE', stageResult: 'FAILURE')",
		"env.QUALITY_RESULT = results['Quality'] == 'fail' ? 'failing' : 'passing'",
	} {
		if !strings.Contains(quality, want) {
			problems = append(problems, "the Quality stage lacks "+want)
		}
	}
	return problems
}

// TestJenkinsfileRunsTheQualityGates: Quality runs ci/quality.sh over the
// image and the Suite's results, lets a red gate fail the build, and is told
// the two mutation rows are judged by the Mutation phase, whose reports exist
// only after it (and not at all without MUTATION or MUTATION_FULL).
func TestJenkinsfileRunsTheQualityGates(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, qualityProblems, map[string]string{
		"gates that read missing mutation reports": replaceOnce(t, jf, " --mutation-deferred", ""),
		"a Quality unit without the image":         replaceOnce(t, jf, "needs: ['res-suite', 'res-image', 'image']", "needs: ['res-suite']"),
		"a red gate that cannot fail the build":    replaceOnce(t, jf, "      catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {\n                env.QUALITY_RESULT", "      catchError(buildResult: 'SUCCESS', stageResult: 'FAILURE') {\n                env.QUALITY_RESULT"),
		"no quality badge result":                  replaceOnce(t, jf, "env.QUALITY_RESULT = results['Quality'] == 'fail' ? 'failing' : 'passing'", ""),
	})
	build := readRepoFile(t, "ci", "image-build.sh")
	if !strings.Contains(build, "docker buildx build") {
		t.Error("ci/image-build.sh does not build the image")
	}
	if !strings.Contains(build, "docker save") {
		t.Error("ci/image-build.sh does not save the image tar the image gates scan")
	}
}

// releaseCandidateProblems: board/ci builds and never releases. Nothing
// pushes, tags or publishes, and a main build that ends SUCCESS (checked in
// post { always }, after every unit, so an UNSTABLE or failed build never
// qualifies) archives the release candidate board/release may copy.
func releaseCandidateProblems(jf string) []string {
	var problems []string
	for _, l := range jenkinsfileLines(jf) {
		for _, bad := range []string{"TAG_NAME", "tag pattern", "gh release", "release-build.sh", "git push", "docker push", "git tag"} {
			if strings.Contains(l, bad) {
				problems = append(problems, "line "+l+" mentions "+bad+": board/ci must not release")
			}
		}
	}
	_, post, ok := strings.Cut(jf, "\n  post {")
	if !ok {
		return append(problems, "there is no pipeline-level post { } block")
	}
	for _, want := range []string{
		"if (currentBuild.currentResult == 'SUCCESS' && env.BRANCH_NAME == 'main') {",
		"writeFile file: 'ci-results/release-candidate.json'",
		`'{"commit": "' + commitSha`,
		`"build_url": "' + env.BUILD_URL`,
		`"build_number": ' + env.BUILD_NUMBER`,
		"archiveArtifacts artifacts: 'ci-results/release-candidate.json'",
		"copyArtifactPermission('/board/release,/board/ci/*')",
	} {
		if !strings.Contains(jf, want) {
			problems = append(problems, "lacks "+want)
		}
	}
	if !strings.Contains(post, "release-candidate.json") {
		problems = append(problems, "the release candidate is not written in post { }")
	}
	return problems
}

// TestJenkinsfileNeverReleases: board/ci builds branches only; releasing is
// board/release's job. A main build that ends SUCCESS archives the release
// candidate, and board/release may copy it.
func TestJenkinsfileNeverReleases(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, releaseCandidateProblems, map[string]string{
		"a pushed commit":                  replaceOnce(t, jf, "        sh 'rm -rf ci-results", "        sh 'git push origin HEAD'\n        sh 'rm -rf ci-results"),
		"a pushed image":                   replaceOnce(t, jf, "        sh 'rm -rf ci-results", "        sh 'docker push board:ci'\n        sh 'rm -rf ci-results"),
		"a release":                        replaceOnce(t, jf, "        sh 'rm -rf ci-results", "        sh 'gh release create v1'\n        sh 'rm -rf ci-results"),
		"a candidate from a red build":     replaceOnce(t, jf, "currentBuild.currentResult == 'SUCCESS' && ", ""),
		"a candidate from any branch":      replaceOnce(t, jf, " && env.BRANCH_NAME == 'main') {\n                writeFile", ") {\n                writeFile"),
		"a candidate without its commit":   replaceOnce(t, jf, `'{"commit": "' + commitSha`, `'{"commit": "' + env.BRANCH_NAME`),
		"a candidate nobody can copy":      replaceOnce(t, jf, "copyArtifactPermission('/board/release,/board/ci/*')", "copyArtifactPermission('/board/ci/*')"),
		"a candidate that is not archived": replaceOnce(t, jf, "archiveArtifacts artifacts: 'ci-results/release-candidate.json'", "echo 'ci-results/release-candidate.json'"),
	})
}

// TestJenkinsfileReleaseValidatesTheCandidateAndStops: board/release is a
// placeholder. It validates VERSION and CI_BUILD against the release candidate
// of a green board/ci main build, checks that commit out and stops: it has a
// DRY_RUN parameter and no other behaviour, publishes nothing, creates no tag
// and holds no credential. It fails if the file starts publishing or drops
// the validation.
func TestJenkinsfileReleaseValidatesTheCandidateAndStops(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile.release")
	want := []string{"Candidate", "Checkout"}
	if got := jenkinsStages(jf); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("release stages = %q, want %q", got, want)
	}
	for _, p := range []string{"VERSION", "CI_BUILD", "DRY_RUN"} {
		if !strings.Contains(jf, "(name: '"+p+"'") {
			t.Errorf("Jenkinsfile.release has no %s parameter", p)
		}
	}
	checks := map[string][]string{
		"Candidate": {
			`params.VERSION ==~ /^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/`,
			`params.CI_BUILD ==~ /^[0-9]+$/`,
			"projectName: '/board/ci/main', filter: 'ci-results/release-candidate.json'",
			"specific(params.CI_BUILD) : lastSuccessful(stable: true)",
			`env.RELEASE_SHA ==~ /^[0-9a-f]{40}$/`,
		},
		"Checkout": {"checkout scm", `git checkout --quiet --detach "$RELEASE_SHA"`},
	}
	for stage, wants := range checks {
		body, ok := jenkinsStage(jf, stage)
		if !ok {
			t.Errorf("Jenkinsfile.release has no %s stage", stage)
			continue
		}
		for _, w := range wants {
			if !strings.Contains(body, w) {
				t.Errorf("the %s stage lacks %q", stage, w)
			}
		}
	}
	for _, l := range jenkinsfileLines(jf) {
		for _, bad := range []string{
			"git push", "git tag", "gh release", "gh api", "docker push", "docker build", "release-build.sh",
			"withCredentials", "GH_TOKEN", "credentialsId", "ci/run.sh", "when {", "stage('Build')", "stage('Publish')",
		} {
			if strings.Contains(l, bad) {
				t.Errorf("Jenkinsfile.release line %q mentions %q: the placeholder validates and stops, it never builds or publishes", l, bad)
			}
		}
		if strings.Contains(l, "params.DRY_RUN") {
			t.Errorf("Jenkinsfile.release line %q branches on DRY_RUN: the placeholder is a dry run whatever it says", l)
		}
	}
}

// TestJenkinsfileReleaseProbeCatchesAPublishingRelease: the release probe is
// not vacuous, so a Jenkinsfile.release that publishes or drops the
// validation fails it. It runs the probe's own checks over doctored copies.
func TestJenkinsfileReleaseProbeCatchesAPublishingRelease(t *testing.T) {
	good := readRepoFile(t, "Jenkinsfile.release")
	if bad := releaseProblems(good); len(bad) != 0 {
		t.Fatalf("the real Jenkinsfile.release has problems: %q", bad)
	}
	doctored := map[string]string{
		"a Publish stage":          strings.Replace(good, "stage('Checkout') {", "stage('Publish') {\n      steps { sh 'gh release create v1' }\n    }\n    stage('Checkout') {", 1),
		"a dropped VERSION check":  strings.Replace(good, `params.VERSION ==~`, `params.VERSION !=`, 1),
		"a dropped CI_BUILD check": strings.Replace(good, `params.CI_BUILD ==~`, `params.CI_BUILD !=`, 1),
		"no candidate copy":        strings.Replace(good, "copyArtifacts projectName", "echo projectName", 1),
		"a pushed tag":             strings.Replace(good, "echo 'Dry run:", "sh 'git push origin v1'\n        echo 'Dry run:", 1),
	}
	for name, text := range doctored {
		if name == "a Publish stage" || name == "a pushed tag" {
			if text == good {
				t.Fatalf("%s: the doctoring changed nothing", name)
			}
		}
		if len(releaseProblems(text)) == 0 {
			t.Errorf("%s was not caught by the release probe", name)
		}
	}
}

// releaseProblems is the placeholder's contract as a pure function of the
// file text: it validates, copies the candidate, and does nothing else.
func releaseProblems(jf string) []string {
	var problems []string
	for _, must := range []string{
		`params.VERSION ==~ /^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/`,
		`params.CI_BUILD ==~ /^[0-9]+$/`,
		"copyArtifacts projectName: '/board/ci/main'",
		"release-candidate.json",
		`env.RELEASE_SHA ==~ /^[0-9a-f]{40}$/`,
	} {
		if !strings.Contains(jf, must) {
			problems = append(problems, "lacks "+must)
		}
	}
	if got := strings.Join(jenkinsStages(jf), ","); got != "Candidate,Checkout" {
		problems = append(problems, "stages "+got)
	}
	for _, l := range jenkinsfileLines(jf) {
		for _, bad := range []string{"git push", "git tag", "gh release", "docker push", "withCredentials"} {
			if strings.Contains(l, bad) {
				problems = append(problems, l)
			}
		}
	}
	return problems
}

// TestJenkinsfilePublishesReports: test results, the Allure report and the
// CI summary are on every build's page, red or green.
func TestJenkinsfilePublishesReports(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	_, post, ok := strings.Cut(jf, "\n  post {")
	if !ok {
		t.Fatal("Jenkinsfile has no pipeline-level post { } block")
	}
	for _, want := range []string{
		"junit testResults: 'ci-results/junit-merged/*.xml'",
		"ci/jenkins-report.sh",
		"reportName: 'Allure'",
		"reportName: 'CI summary'",
		"addSummary(",
		"ci-results/*.cdx.json",
	} {
		if !strings.Contains(post, want) {
			t.Errorf("post { always } lacks %q", want)
		}
	}
}

// TestJenkinsfileDeclaresTheReadmeBadges: every badge the README shows from
// the public badge proxy is the plugin's "build" or declared by the
// Jenkinsfile, so none of them renders as "not run" forever, and each is an
// image served from the public badge path. The README shows all four, each
// linked to the board/ci/main job as deck's are (see
// TestReadmeBadgesLinkToTheMainJob).
func TestJenkinsfileDeclaresTheReadmeBadges(t *testing.T) {
	const prefix = "https://int21h.mooo.com/jenkins-static/board/ci/main/"
	readme := readRepoFile(t, "README.md")
	jf := readRepoFile(t, "Jenkinsfile")
	shown := map[string]bool{}
	for _, l := range strings.Split(readme, "\n") {
		_, rest, ok := strings.Cut(l, "("+prefix)
		if !ok {
			continue
		}
		// The image is wrapped in a link, checked by TestReadmeBadgesLinkToTheMainJob.
		l, _, _ = strings.Cut(strings.TrimPrefix(l, "["), ")](")
		l = strings.TrimSuffix(l, ")") + ")"
		id, _, ok := strings.Cut(rest, ".svg)")
		if !ok {
			t.Errorf("README badge line %q is not <prefix><id>.svg", l)
			continue
		}
		shown[id] = true
		if want := "![" + id + "](" + prefix + id + ".svg)"; strings.TrimSpace(l) != want {
			t.Errorf("README badge line %q, want exactly %q", l, want)
		}
		if id == "build" {
			continue
		}
		if !strings.Contains(jf, "addEmbeddableBadgeConfiguration(id: '"+id+"'") {
			t.Errorf("README shows badge %q but the Jenkinsfile never declares it", id)
		}
	}
	for _, id := range []string{"build", "tests", "coverage", "quality"} {
		if !shown[id] {
			t.Errorf("README does not show the %s badge from %s", id, prefix)
		}
	}
}

// TestJenkinsfilesNotifyTelegram: both jobs load the homelab shared library
// and post their result to Telegram from post { cleanup }, which runs after
// every other post condition, so the message carries the final result and is
// the last thing in post.
func TestJenkinsfilesNotifyTelegram(t *testing.T) {
	for _, name := range []string{"Jenkinsfile", "Jenkinsfile.release"} {
		jf := readRepoFile(t, name)
		lib := strings.Index(jf, "@Library('homelab') _")
		if lib < 0 || lib > strings.Index(jf, "\npipeline {") {
			t.Errorf("%s does not load @Library('homelab') before the pipeline", name)
		}
		_, post, ok := strings.Cut(jf, "\n  post {")
		if !ok {
			t.Errorf("%s has no pipeline-level post { } block", name)
			continue
		}
		_, cleanup, ok := strings.Cut(post, "\n    cleanup {")
		if !ok || !strings.Contains(cleanup, "notifyTelegram(notifyOn: 'ALL'") {
			t.Errorf("%s does not call notifyTelegram(notifyOn: 'ALL', ...) in post { cleanup }", name)
		}
		if strings.Contains(jf, "100.71.162.65") {
			t.Errorf("%s still talks to the notifier itself; notifyTelegram owns that", name)
		}
	}
}

// mutationGateProblems says what is wrong with the Mutation phase's `when`:
// it is gated on CI_MUTATION (set by MUTATION or MUTATION_FULL or the timer)
// and on no earlier phase having failed, and on nothing else. EXTENDED alone
// sets CI_NIGHTLY, not CI_MUTATION, so it runs no mutation, and no branch
// condition decides it.
func mutationGateProblems(stageBody string) []string {
	var problems []string
	lines := jenkinsfileLines(stageBody)
	if !anyLineContains(lines, "expression { env.CI_MUTATION == 'true' && currentBuild.currentResult != 'FAILURE' }") {
		problems = append(problems, "lacks the CI_MUTATION guard behind a passing earlier phase")
	}
	for _, banned := range []string{"CI_NIGHTLY", "EXTENDED", "anyOf", "branch "} {
		if anyLineContains(lines, banned) {
			problems = append(problems, "carries "+banned+", so a build without MUTATION or MUTATION_FULL could run it")
		}
	}
	return problems
}

// mutationModeProblems: how the Jenkinsfile picks the mutation mode, and what
// the diff-mode unit and the base it diffs from look like (full mode is
// fullMutationProblems).
func mutationModeProblems(jf string) []string {
	var problems []string
	for _, want := range []string{
		"boolean full = params.MUTATION_FULL || timer",
		"env.CI_MUTATION = (params.MUTATION || full) ? 'true' : 'false'",
		"env.CI_MUTATION_MODE = full ? 'full' : 'diff'",
		"if (env.CI_MUTATION_MODE == 'full') {",
		"ci-results/mutation/*.json",
		// Diff mode: one 45-minute unit over the lines changed since the base.
		"u = [name: 'Mutation diff', minutes: 45,",
		"mutationBase = mutationDiffBase()",
		`"BOARD_MUTATION_DIFF=${mutationBase} " + tune +`,
		" ci/run.sh ci/mutation.sh ci-results/mutation ci/quality.json all'",
		"catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {\n              runUnit(u)",
	} {
		if !strings.Contains(jf, want) {
			problems = append(problems, "lacks "+want)
		}
	}
	// The diff base: the merge base with origin/<CHANGE_TARGET or main>
	// (fetched first), HEAD~1 on main itself.
	_, rest, ok := strings.Cut(jf, "String mutationDiffBase() {")
	if !ok {
		return append(problems, "has no mutationDiffBase()")
	}
	base, _, _ := strings.Cut(rest, "\n}\n")
	for _, want := range []string{
		"if (env.BRANCH_NAME == 'main') {",
		"git rev-parse HEAD~1",
		"String target = env.CHANGE_TARGET ?: 'main'",
		"git fetch --no-tags --quiet origin +refs/heads/${target}:refs/remotes/origin/${target}",
		`git merge-base origin/${target} HEAD`,
	} {
		if !strings.Contains(base, want) {
			problems = append(problems, "the diff base lacks "+want)
		}
	}
	return problems
}

// TestJenkinsfileMutationStageRunsTheMutationGate: the Mutation phase comes
// last, after Core (Quality) and Stability, is gated on CI_MUTATION and on
// nothing else, picks full or diff mode from MUTATION_FULL, MUTATION and the
// timer, runs diff mode as one 45-minute unit over the lines changed since the
// merge base with origin/<CHANGE_TARGET or main> (HEAD~1 on main), runs
// `ci/run.sh ci/mutation.sh` as its command, and lets a red gate fail the build.
func TestJenkinsfileMutationStageRunsTheMutationGate(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	stages := topStages(jf)
	if len(stages) == 0 || stages[len(stages)-1] != "Mutation" {
		t.Fatalf("stages = %q, want Mutation last", stages)
	}
	body, _ := jenkinsStage(jf, "Mutation")
	for _, problem := range mutationGateProblems(body) {
		t.Errorf("the Mutation stage's when {}: %s:\n%s", problem, body)
	}
	expectProblems(t, jf, mutationModeProblems, map[string]string{
		"full mode without the timer":      replaceOnce(t, jf, "params.MUTATION_FULL || timer", "params.MUTATION_FULL"),
		"EXTENDED running mutation":        replaceOnce(t, jf, "(params.MUTATION || full) ?", "(params.MUTATION || full || params.EXTENDED) ?"),
		"the timer not running mutation":   replaceOnce(t, jf, "(params.MUTATION || full) ? 'true'", "(params.MUTATION || params.MUTATION_FULL) ? 'true'"),
		"every mutation run full":          replaceOnce(t, jf, "env.CI_MUTATION_MODE = full ? 'full' : 'diff'", "env.CI_MUTATION_MODE = 'full'"),
		"a diff unit with another limit":   replaceOnce(t, jf, "name: 'Mutation diff', minutes: 45,", "name: 'Mutation diff', minutes: 90,"),
		"a diff run over every line":       replaceOnce(t, jf, `"BOARD_MUTATION_DIFF=${mutationBase} " + tune +`, `"" + tune +`),
		"a diff run with no base":          replaceOnce(t, jf, "mutationBase = mutationDiffBase()", "mutationBase = 'HEAD'"),
		"a main build diffing from itself": replaceOnce(t, jf, "git rev-parse HEAD~1", "git rev-parse HEAD"),
		"a main build checked by branch":   replaceOnce(t, jf, "if (env.BRANCH_NAME == 'main') {\n    return", "if (env.BRANCH_NAME == 'dev') {\n    return"),
		"a branch diffing from main only":  replaceOnce(t, jf, "env.CHANGE_TARGET ?: 'main'", "'main'"),
		"a base that is the target's tip":  replaceOnce(t, jf, `git merge-base origin/${target} HEAD`, `git rev-parse origin/${target}`),
		"a base never fetched":             replaceOnce(t, jf, "git fetch --no-tags --quiet origin +refs/heads/${target}:refs/remotes/origin/${target}", "true"),
		"a red mutation gate that passes":  replaceOnce(t, jf, "catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {\n              runUnit(u)", "catchError(buildResult: 'SUCCESS', stageResult: 'FAILURE') {\n              runUnit(u)"),
		"reports that are not archived":    replaceOnce(t, jf, " ci-results/mutation/*.json,", ""),
	})
}

// TestMutationGateProblemsRejectsWrongGating: the guard check fails a Mutation
// phase that runs on EXTENDED alone (the CI_NIGHTLY guard Stability uses), on a
// push build of main, behind an anyOf, or with no guard at all, and passes the
// CI_MUTATION guard.
func TestMutationGateProblemsRejectsWrongGating(t *testing.T) {
	stage := func(when string) string {
		return "stage('Mutation') {\n when {\n" + when + "\n }\n steps { sh 'x' }\n}\n"
	}
	ok := stage("beforeAgent true\nexpression { env.CI_MUTATION == 'true' && currentBuild.currentResult != 'FAILURE' }")
	if got := mutationGateProblems(ok); len(got) != 0 {
		t.Errorf("the CI_MUTATION guard is refused: %v", got)
	}
	for name, body := range map[string]string{
		"the Stability guard (EXTENDED alone)": stage("expression { env.CI_NIGHTLY == 'true' && currentBuild.currentResult != 'FAILURE' }"),
		"a push build of main":                 stage("anyOf {\nbranch 'main'\nexpression { env.CI_MUTATION == 'true' && currentBuild.currentResult != 'FAILURE' }\n}"),
		"no guard":                             "stage('Mutation') {\n steps { sh 'x' }\n}\n",
		"no earlier-phase check":               stage("expression { env.CI_MUTATION == 'true' }"),
		"EXTENDED read directly":               stage("expression { params.EXTENDED }"),
	} {
		if got := mutationGateProblems(body); len(got) == 0 {
			t.Errorf("%s passes", name)
		}
	}
}

// fullMutationProblems: full mode (MUTATION_FULL or the timer) is ONE unit
// running `ci/mutation.sh ci-results/mutation ci/quality.json all` (every
// package plus Stryker, no lane), 360 minutes, a single attempt, with the
// worker split nproc/2 gremlins workers at GOMAXPROCS 2 (nice 10 is
// ci/mutation.sh's).
func fullMutationProblems(jf string) []string {
	var problems []string
	body, ok := jenkinsStage(jf, "Mutation")
	if !ok {
		return []string{"there is no Mutation stage"}
	}
	for _, want := range []string{
		"u = [name: 'Mutation full', minutes: 360, maxAttempts: 1,",
		"body: { sh(returnStatus: true, script: tune + ' ci/run.sh ci/mutation.sh ci-results/mutation ci/quality.json all') }]",
		"String tune = 'BOARD_MUTATION_WORKERS=$(( $(nproc) / 2 )) BOARD_MUTATION_TEST_CPU=2'",
	} {
		if !strings.Contains(body, want) {
			problems = append(problems, "the Mutation stage lacks "+want)
		}
	}
	if n := strings.Count(body, "ci/mutation.sh ci-results/mutation ci/quality.json"); n != 2 {
		problems = append(problems, "the Mutation stage runs ci/mutation.sh "+strconv.Itoa(n)+" times, want one per mode")
	}
	for _, lane := range []string{"base", "logic", "server-ops", "server-streams", "server-core", "cli-core", "cli "} {
		if strings.Contains(body, "ci/quality.json "+lane) {
			problems = append(problems, "the Mutation stage runs lane "+lane+" instead of every package in one unit")
		}
	}
	return problems
}

// TestFullMutationIsOneUnitOverEveryPackage: the seven serial Mutation stages
// are gone. Full mode is one unit over lane `all`, which ci/mutationgate
// resolves to every unit of ci/quality.json's mutation_go.packages whatever
// mutation_go.lanes says, with the 360-minute limit and one attempt. The lanes
// remain a way to run part of it by hand, and must still partition the
// packages so `all` and a lane never disagree.
func TestFullMutationIsOneUnitOverEveryPackage(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, fullMutationProblems, map[string]string{
		"a shorter limit":              replaceOnce(t, jf, "minutes: 360, maxAttempts: 1", "minutes: 120, maxAttempts: 1"),
		"a longer limit":               replaceOnce(t, jf, "minutes: 360, maxAttempts: 1", "minutes: 480, maxAttempts: 1"),
		"more attempts":                replaceOnce(t, jf, "maxAttempts: 1,\n", "maxAttempts: 4,\n"),
		"a lane instead of all":        replaceOnce(t, jf, "ci-results/mutation ci/quality.json all') }]", "ci-results/mutation ci/quality.json base') }]"),
		"a serial lane stage":          replaceOnce(t, jf, "ci-results/mutation ci/quality.json all') }]", "ci-results/mutation ci/quality.json server-ops') }]"),
		"a different worker split":     replaceOnce(t, jf, "BOARD_MUTATION_WORKERS=$(( $(nproc) / 2 ))", "BOARD_MUTATION_WORKERS=$(( $(nproc) ))"),
		"a different per-worker CPU":   replaceOnce(t, jf, "BOARD_MUTATION_TEST_CPU=2'", "BOARD_MUTATION_TEST_CPU=1'"),
		"a unit named for another run": replaceOnce(t, jf, "name: 'Mutation full'", "name: 'Mutation'"),
	})
	var all struct {
		MutationGo struct {
			Packages []string            `json:"packages"`
			Lanes    map[string][]string `json:"lanes"`
		} `json:"mutation_go"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, "ci", "quality.json")), &all); err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, pkgs := range all.MutationGo.Lanes {
		for _, p := range pkgs {
			pkg, _, _ := strings.Cut(p, "#") // a unit "internal/server#ops" measures part of internal/server
			covered[pkg] = true
		}
	}
	for _, p := range all.MutationGo.Packages {
		if !covered[p] {
			t.Errorf("%s is in no lane of ci/quality.json, so `ci/mutation.sh ... <lane>` could never measure it", p)
		}
	}
	if !strings.Contains(readRepoFile(t, "ci", "mutation.sh"), "lane=${3:-all}") {
		t.Error("ci/mutation.sh no longer defaults to the lane all")
	}
	if !strings.Contains(readRepoFile(t, "ci", "mutation.sh"), "nice -n 10 gremlins unleash") {
		t.Error("ci/mutation.sh no longer runs gremlins at nice 10")
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

var badgeLinkRE = regexp.MustCompile(`^\[!\[(\w+)\]\(https://[^)]*/board/ci/main/(\w+)\.svg\)\]\((https://[^)\s]+)\)$`)

// TestReadmeBadgesLinkToTheMainJob: each of the four README badges is an image
// wrapped in a link to the board/ci/main job's last completed build, the way
// deck's are: build to the build, tests to Allure, coverage and quality to the
// CI summary. A badge that loses its link, or whose link stops pointing at
// /job/board/job/ci/job/main/, fails.
func TestReadmeBadgesLinkToTheMainJob(t *testing.T) {
	want := map[string]string{
		"build":    "/job/board/job/ci/job/main/lastCompletedBuild/",
		"tests":    "/job/board/job/ci/job/main/lastCompletedBuild/Allure/",
		"coverage": "/job/board/job/ci/job/main/lastCompletedBuild/CI_20summary/",
		"quality":  "/job/board/job/ci/job/main/lastCompletedBuild/CI_20summary/",
	}
	for id, problem := range badgeLinkProblems(readRepoFile(t, "README.md"), want) {
		t.Errorf("README badge %s: %s", id, problem)
	}
}

// badgeLinkProblems maps a badge id to what is wrong with its README line:
// missing, unlinked, or linked somewhere other than the expected job path.
func badgeLinkProblems(readme string, want map[string]string) map[string]string {
	problems := map[string]string{}
	seen := map[string]bool{}
	for _, l := range strings.Split(readme, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "[![") && !strings.HasPrefix(l, "![") {
			continue
		}
		m := badgeLinkRE.FindStringSubmatch(l)
		if m == nil {
			for id := range want {
				if strings.Contains(l, "/board/ci/main/"+id+".svg") {
					seen[id] = true
					problems[id] = "has no link around its image: " + l
				}
			}
			continue
		}
		id, img, link := m[1], m[2], m[3]
		seen[id] = true
		path, ok := want[id]
		switch {
		case !ok:
		case img != id:
			problems[id] = "image is " + img + ".svg"
		case !strings.HasSuffix(link, path) || len(link) == len("https://")+len(path):
			problems[id] = "links to " + link + ", want a host followed by " + path
		}
	}
	for id := range want {
		if !seen[id] {
			problems[id] = "missing"
		}
	}
	return problems
}

func TestBadgeLinkProbeCatchesAnUnlinkedOrMisdirectedBadge(t *testing.T) {
	want := map[string]string{"build": "/job/board/job/ci/job/main/lastCompletedBuild/"}
	const img = "https://b.example/jenkins-static/board/ci/main/build.svg"
	good := "[![build](" + img + ")](https://j.example/job/board/job/ci/job/main/lastCompletedBuild/)"
	if p := badgeLinkProblems(good, want); len(p) != 0 {
		t.Errorf("good line flagged: %v", p)
	}
	for name, line := range map[string]string{
		"unlinked":  "![build](" + img + ")",
		"wrong job": "[![build](" + img + ")](https://j.example/job/deck/job/ci/job/main/lastCompletedBuild/)",
		"absent":    "",
	} {
		if p := badgeLinkProblems(line, want); p["build"] == "" {
			t.Errorf("%s: not flagged", name)
		}
	}
}
