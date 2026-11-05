// jenkinsfile_test.go probes the Jenkinsfile (board/ci) and Jenkinsfile.release
// (board/release), carried from deck's ci/repocheck/jenkinsfile_test.go: the CI
// stages run in the documented order, the extended lane is scheduled on main,
// the quality gates and reports stay wired in, only a fully green main build
// leaves a release candidate, and the release job is a placeholder that
// validates against that candidate and publishes nothing. Plain text matching;
// both files are small and declarative.
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

// TestJenkinsfileStagesRunInOrder: the seven stages of SPEC 15.3 exist and
// run in order, so removing one fails here, and each runs its command.
func TestJenkinsfileStagesRunInOrder(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	want := []string{"CI image", "Lint", "Suite", "Allure smoke", "Quality", "Stability", "Mutation"}
	if got := jenkinsStages(jf); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("stages = %q, want %q", got, want)
	}
	commands := map[string]string{
		"CI image":     "sh 'ci/jenkins-image.sh'",
		"Lint":         "ci/run.sh ci/lint.sh",
		"Suite":        "ci/run.sh ci/suite.sh",
		"Allure smoke": "ci/allure-smoke.sh allure-smoke/fixture allure-smoke/report",
		"Quality":      "ci/run.sh ci/quality.sh",
		"Stability":    "ci/stability.sh 3",
		"Mutation":     "ci/run.sh ci/mutation.sh",
	}
	for stage, cmd := range commands {
		body, ok := jenkinsStage(jf, stage)
		if !ok || !strings.Contains(body, cmd) {
			t.Errorf("the %s stage does not run %q", stage, cmd)
		}
	}
	for _, v := range []string{"BOARD_CI_CACHE_VOLUME", "BOARD_CI_NPM_VOLUME"} {
		if !strings.Contains(readRepoFile(t, "ci", "jenkins-image.sh"), `fix_volume "$`+v+`"`) {
			t.Errorf("ci/jenkins-image.sh does not make $%s usable by the agent's uid", v)
		}
	}
	if !strings.Contains(readRepoFile(t, "ci", "jenkins-image.sh"), "docker build -t board-ci:local -f ci/Dockerfile ci") {
		t.Error("ci/jenkins-image.sh does not build board-ci:local from ci/Dockerfile")
	}
	smoke, _ := jenkinsStage(jf, "Allure smoke")
	if !strings.Contains(smoke, "-run '^TestAllureFormatterWritesResultsForAFixtureRun$' ./features/") {
		t.Error("the Allure smoke stage does not run the Godog formatter fixture")
	}
}

// TestJenkinsfileDeclaresTheJobShape: the library, the agent label, serial
// builds, a clean workspace, the EXTENDED parameter and the nightly trigger.
func TestJenkinsfileDeclaresTheJobShape(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	lib := strings.Index(jf, "@Library('homelab') _")
	if lib < 0 || lib > strings.Index(jf, "\npipeline {") {
		t.Error("the Jenkinsfile does not load @Library('homelab') before the pipeline")
	}
	for _, want := range []string{
		"agent { label 'linux && amd64 && docker' }",
		"disableConcurrentBuilds()",
		"git clean -ffdxq",
		"booleanParam(name: 'EXTENDED', defaultValue: false",
		// ci/run.sh mounts both cache volumes; the agent's uid owns neither
		// until ci/jenkins-image.sh chowns them.
		"BOARD_CI_CACHE_VOLUME = 'board-go-cache-jenkins'",
		"BOARD_CI_NPM_VOLUME = 'board-npm-cache-jenkins'",
	} {
		if !strings.Contains(jf, want) {
			t.Errorf("the Jenkinsfile lacks %q", want)
		}
	}
}

// TestJenkinsfileNightlyLane: main is built nightly at 03:17, and a nightly
// (or EXTENDED) build adds -race, drops the vulnerability databases and runs
// three stability runs.
func TestJenkinsfileNightlyLane(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	if !strings.Contains(jf, `cron(env.BRANCH_NAME == 'main' ? '17 3 * * *' : '')`) {
		t.Error("the Jenkinsfile does not schedule main nightly at 17 3 * * *")
	}
	if !strings.Contains(jf, "TimerTrigger$TimerTriggerCause") || !strings.Contains(jf, "params.EXTENDED") {
		t.Error("the extended lane is not selected from the timer cause and the EXTENDED parameter")
	}
	if !strings.Contains(jf, "env.BOARD_CI_GO_EXTRA_FLAGS = nightly ? '-race' : ''") {
		t.Error("a nightly build does not set BOARD_CI_GO_EXTRA_FLAGS=-race")
	}
	quality, _ := jenkinsStage(jf, "Quality")
	if !strings.Contains(quality, "rm -rf /go-cache/trivy/db /go-cache/trivy/java-db /go-cache/xdg-cache/govulncheck") {
		t.Error("the nightly lane no longer drops the cached vulnerability databases before the quality gates")
	}
	stability, _ := jenkinsStage(jf, "Stability")
	if !strings.Contains(stability, "environment name: 'CI_NIGHTLY', value: 'true'") || !strings.Contains(stability, "ci/stability.sh 3") {
		t.Error("the Stability stage does not run ci/stability.sh 3 on nightly builds only")
	}
}

// TestJenkinsfileRunsTheQualityGates: the Quality stage builds the image the
// image gates scan, runs ci/quality.sh over the results, and lets a red gate
// fail the build. Its mutation rows are judged by the Mutation stage, which
// runs after it (and not at all on a branch), so quality.sh is told so.
func TestJenkinsfileRunsTheQualityGates(t *testing.T) {
	quality, ok := jenkinsStage(readRepoFile(t, "Jenkinsfile"), "Quality")
	if !ok {
		t.Fatal("the Jenkinsfile has no Quality stage")
	}
	for _, want := range []string{
		`sh 'ci/image-build.sh "$BOARD_CI_OUT/board-image.tar"'`,
		`sh 'ci/image-check.sh "$BOARD_CI_OUT"'`,
		`ci/run.sh ci/quality.sh "$BOARD_CI_OUT" ci/quality.json --mutation-deferred`,
		"catchError(buildResult: 'FAILURE'",
	} {
		if !strings.Contains(quality, want) {
			t.Errorf("the Quality stage lacks %q", want)
		}
	}
	if !strings.Contains(readRepoFile(t, "ci", "image-build.sh"), "docker buildx build") {
		t.Error("ci/image-build.sh does not build the image")
	}
	if !strings.Contains(readRepoFile(t, "ci", "image-build.sh"), "docker save") {
		t.Error("ci/image-build.sh does not save the image tar the image gates scan")
	}
}

// TestJenkinsfileNeverReleases: board/ci builds branches only; releasing is
// board/release's job. A main build that ends SUCCESS (post success {}, which
// runs after always {}, so an UNSTABLE build never qualifies) archives the
// release candidate, and board/release may copy it.
func TestJenkinsfileNeverReleases(t *testing.T) {
	jf := readRepoFile(t, "Jenkinsfile")
	for _, l := range jenkinsfileLines(jf) {
		for _, bad := range []string{"TAG_NAME", "tag pattern", "gh release", "release-build.sh", "git push", "docker push"} {
			if strings.Contains(l, bad) {
				t.Errorf("Jenkinsfile line %q mentions %q: board/ci must not release", l, bad)
			}
		}
	}
	_, post, ok := strings.Cut(jf, "\n  post {")
	if !ok {
		t.Fatal("Jenkinsfile has no pipeline-level post { } block")
	}
	_, success, ok := strings.Cut(post, "\n    success {")
	if !ok {
		t.Fatal("the release candidate is not written in post { success { } }")
	}
	for _, want := range []string{
		"env.BRANCH_NAME == 'main'",
		"writeFile file: 'ci-results/release-candidate.json'",
		`'{"commit": "' + env.GIT_COMMIT`,
		`"build_url": "' + env.BUILD_URL`,
		`"build_number": ' + env.BUILD_NUMBER`,
		"archiveArtifacts artifacts: 'ci-results/release-candidate.json'",
	} {
		if !strings.Contains(success, want) {
			t.Errorf("post { success } lacks %q", want)
		}
	}
	if !strings.Contains(jf, "copyArtifactPermission('/board/release,/board/ci/*')") {
		t.Error("board/release (candidate) and board/ci (Allure history) are not both allowed to copy artifacts")
	}
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
