// imageversion_test.go: the image reports what it was built as (SPEC 13, 8.3).
// The Dockerfile declares ARG VERSION and ARG REVISION and passes them to the
// Go build as the two -X linker flags internal/version reads, ci/image-build.sh
// hands both build args to docker, and the Jenkinsfile sets them to the tag or
// the short sha and the full commit.
package repocheck

import (
	"strings"
	"testing"
)

const versionPkg = "github.com/n-orlov/board/internal/version"

// versionStampProblems returns every way a Dockerfile fails to stamp VERSION
// and REVISION into the binary: a build stage without either ARG, or a go build
// that does not pass them as -X <pkg>.version= and -X <pkg>.revision=.
func versionStampProblems(dockerfile string) []string {
	var problems []string
	declared := map[string]bool{}
	passed := map[string]bool{}
	for _, s := range dockerfileStages(dockerfile) {
		for _, arg := range s.each("ARG") {
			if len(arg) > 1 {
				name, _, _ := strings.Cut(arg[1], "=")
				declared[name] = true
			}
		}
		for _, run := range s.each("RUN") {
			line := strings.Join(run, " ")
			if !strings.Contains(line, "go build") {
				continue
			}
			for arg, flag := range map[string]string{"VERSION": ".version=", "REVISION": ".revision="} {
				for _, ref := range []string{"$" + arg, "${" + arg + "}"} {
					if strings.Contains(line, "-X "+versionPkg+flag+ref) {
						passed[arg] = true
					}
				}
			}
		}
	}
	for _, arg := range []string{"VERSION", "REVISION"} {
		if !declared[arg] {
			problems = append(problems, "the Dockerfile declares no ARG "+arg)
		}
		if !passed[arg] {
			problems = append(problems, "the go build does not pass $"+arg+" as -X "+versionPkg+"."+strings.ToLower(arg))
		}
	}
	return problems
}

const stampedDockerfile = `FROM --platform=$BUILDPLATFORM golang:1.26@sha256:bb AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=devel
ARG REVISION=
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w -X github.com/n-orlov/board/internal/version.version=$VERSION -X github.com/n-orlov/board/internal/version.revision=${REVISION}" -o /out/board ./cmd/board
FROM scratch
COPY --from=build /out/board /board
`

func TestVersionStampProblemsAcceptsAStampingDockerfile(t *testing.T) {
	if got := versionStampProblems(stampedDockerfile); len(got) != 0 {
		t.Errorf("flagged %q", got)
	}
}

func TestVersionStampProblemsFlagsEachDeparture(t *testing.T) {
	cases := []struct{ name, from, to, want string }{
		{"no ARG VERSION", "ARG VERSION=devel\n", "", "no ARG VERSION"},
		{"no ARG REVISION", "ARG REVISION=\n", "", "no ARG REVISION"},
		{"version not passed", "-X " + versionPkg + ".version=$VERSION", "", "does not pass $VERSION"},
		{"revision not passed", "-X " + versionPkg + ".revision=${REVISION}", "", "does not pass $REVISION"},
		{"a literal instead of the arg", ".version=$VERSION", ".version=v1", "does not pass $VERSION"},
		{"the wrong package", versionPkg + ".revision=", "example.com/other.revision=", "does not pass $REVISION"},
		{"the wrong variable", ".revision=${REVISION}", ".revision=${VERSION}", "does not pass $REVISION"},
		{"flag lost its -X", "-X " + versionPkg + ".version", versionPkg + ".version", "does not pass $VERSION"},
	}
	for _, tc := range cases {
		mutated := strings.Replace(stampedDockerfile, tc.from, tc.to, 1)
		if mutated == stampedDockerfile {
			t.Fatalf("%s: the mutation changed nothing", tc.name)
		}
		got := strings.Join(versionStampProblems(mutated), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: problems %q, want one containing %q", tc.name, got, tc.want)
		}
	}
	if got := versionStampProblems(""); len(got) != 4 {
		t.Errorf("an empty Dockerfile: %q, want four problems", got)
	}
}

// TestRootDockerfileStampsVersionAndRevision: the committed Dockerfile.
func TestRootDockerfileStampsVersionAndRevision(t *testing.T) {
	for _, problem := range versionStampProblems(readRepoFile(t, "Dockerfile")) {
		t.Errorf("Dockerfile: %s", problem)
	}
}

// TestImageBuildAndJenkinsfilePassTheVersionArgs: the tag or the short sha, and
// the commit, reach both docker builds as the two build args.
func TestImageBuildAndJenkinsfilePassTheVersionArgs(t *testing.T) {
	skipOnDistributedExperiment(t)
	build := readRepoFile(t, "ci", "image-build.sh")
	if n := strings.Count(build, `--build-arg "VERSION=$version" --build-arg "REVISION=$revision"`); n != 2 {
		t.Errorf("ci/image-build.sh passes VERSION and REVISION to %d of its 2 docker builds", n)
	}
	for _, want := range []string{"BOARD_IMAGE_VERSION", "BOARD_IMAGE_REVISION"} {
		if !strings.Contains(build, want) {
			t.Errorf("ci/image-build.sh does not read %s", want)
		}
	}
	jf := readRepoFile(t, "Jenkinsfile")
	for _, want := range []string{
		"env.BOARD_IMAGE_VERSION = sh(returnStdout: true, script: 'git describe --tags --exact-match HEAD 2>/dev/null || git rev-parse --short=7 HEAD')",
		"env.BOARD_IMAGE_REVISION = env.GIT_COMMIT",
	} {
		if !strings.Contains(jf, want) {
			t.Errorf("the Jenkinsfile lacks %q", want)
		}
	}
	if strings.Index(jf, "env.BOARD_IMAGE_VERSION =") > strings.Index(jf, "sh 'ci/image-build.sh") {
		t.Error("the Jenkinsfile sets the image version after building the image")
	}
}
