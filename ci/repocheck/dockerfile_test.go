// dockerfile_test.go: the root Dockerfile builds the container image of SPEC
// 12.4 as a cross-compiling multi-stage build, so the linux/amd64 and
// linux/arm64 images are built without emulation: every builder stage runs on
// $BUILDPLATFORM, the Go build takes GOOS and GOARCH from the target platform,
// the final stage is scratch or distroless static and only COPYs, it carries
// CA certificates and tzdata, and it runs as a non-root user. (The digest pins
// are dockerfiledigest_test.go's.)
package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

// stage is one FROM and the instructions up to the next FROM.
type stage struct {
	from         []string // the FROM line's fields after FROM
	instructions [][]string
}

func (s stage) image() string {
	for _, f := range s.from {
		if !strings.HasPrefix(f, "--") {
			return f
		}
	}
	return ""
}

func (s stage) hasPlatformFlag(want string) bool {
	for _, f := range s.from {
		if f == "--platform="+want {
			return true
		}
	}
	return false
}

// each returns the instructions of one kind (COPY, RUN, USER...), upper-cased match.
func (s stage) each(kind string) [][]string {
	var out [][]string
	for _, in := range s.instructions {
		if strings.EqualFold(in[0], kind) {
			out = append(out, in)
		}
	}
	return out
}

// dockerfileStages splits a Dockerfile into stages. Comments and blank lines
// are dropped and a trailing backslash joins the next line.
func dockerfileStages(dockerfile string) []stage {
	var stages []stage
	var pending string
	for _, raw := range strings.Split(dockerfile, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			pending += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		fields := strings.Fields(pending + line)
		pending = ""
		switch {
		case strings.EqualFold(fields[0], "FROM"):
			stages = append(stages, stage{from: fields[1:]})
		case len(stages) > 0:
			last := &stages[len(stages)-1]
			last.instructions = append(last.instructions, fields)
		}
	}
	return stages
}

// imageProblems returns every way a Dockerfile falls short of SPEC 12.4's
// container: a missing $BUILDPLATFORM on a builder stage, a final stage that
// is not scratch or distroless static, a RUN in it, a root or missing USER,
// missing CA certificates or tzdata, a Go build that does not cross-compile.
func imageProblems(dockerfile string) []string {
	stages := dockerfileStages(dockerfile)
	if len(stages) < 2 {
		return []string{"the Dockerfile is not multi-stage"}
	}
	final := stages[len(stages)-1]
	problems := builderProblems(stages[:len(stages)-1])
	problems = append(problems, finalProblems(final)...)
	return problems
}

func builderProblems(builders []stage) []string {
	var problems []string
	crossCompiles := false
	for _, s := range builders {
		if !s.hasPlatformFlag("$BUILDPLATFORM") {
			problems = append(problems, fmt.Sprintf("builder stage FROM %s lacks --platform=$BUILDPLATFORM", strings.Join(s.from, " ")))
		}
		for _, run := range s.each("RUN") {
			line := strings.Join(run, " ")
			if strings.Contains(line, "go build") && strings.Contains(line, "GOARCH=$TARGETARCH") && strings.Contains(line, "GOOS=$TARGETOS") {
				crossCompiles = true
			}
		}
	}
	if !crossCompiles {
		problems = append(problems, "no builder stage runs go build with GOOS=$TARGETOS GOARCH=$TARGETARCH")
	}
	return problems
}

func finalProblems(final stage) []string {
	var problems []string
	image := strings.ToLower(final.image())
	if image != "scratch" && !strings.HasPrefix(image, "gcr.io/distroless/static") {
		problems = append(problems, fmt.Sprintf("the final stage is FROM %s, want scratch or distroless static", final.image()))
	}
	if len(final.each("RUN")) > 0 {
		problems = append(problems, "the final stage has a RUN, which would need emulation to cross-build")
	}
	problems = append(problems, userProblems(final)...)
	copies := copiedPaths(final)
	if !strings.Contains(copies, "/etc/ssl/certs/ca-certificates.crt") {
		problems = append(problems, "the final stage does not carry CA certificates")
	}
	return problems
}

func userProblems(final stage) []string {
	users := final.each("USER")
	if len(users) == 0 || len(users[len(users)-1]) < 2 {
		return []string{"the final stage sets no USER, so it runs as root"}
	}
	name, _, _ := strings.Cut(users[len(users)-1][1], ":")
	if name == "0" || name == "root" {
		return []string{fmt.Sprintf("the final stage runs as root (USER %s)", users[len(users)-1][1])}
	}
	return nil
}

func copiedPaths(s stage) string {
	var parts []string
	for _, in := range s.each("COPY") {
		parts = append(parts, strings.Join(in, " "))
	}
	return strings.Join(parts, "\n")
}

// carriesTimezones: tzdata is either embedded in the binary (-tags timetzdata)
// or copied into the final stage.
func carriesTimezones(dockerfile string) bool {
	stages := dockerfileStages(dockerfile)
	if len(stages) == 0 {
		return false
	}
	for _, s := range stages[:len(stages)-1] {
		for _, run := range s.each("RUN") {
			if strings.Contains(strings.Join(run, " "), "-tags timetzdata") {
				return true
			}
		}
	}
	return strings.Contains(copiedPaths(stages[len(stages)-1]), "zoneinfo")
}

const goodDockerfile = `FROM --platform=$BUILDPLATFORM node:24@sha256:aa AS web
RUN npm ci
FROM --platform=$BUILDPLATFORM golang:1.26@sha256:bb AS build
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -tags timetzdata -o /out/board ./cmd/vh
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/board /board
USER 65532:65532
ENTRYPOINT ["/board"]
`

func TestImageProblemsAcceptsACrossCompilingScratchImage(t *testing.T) {
	if got := imageProblems(goodDockerfile); len(got) != 0 {
		t.Errorf("flagged %q", got)
	}
	if !carriesTimezones(goodDockerfile) {
		t.Error("tzdata embedded with -tags timetzdata was not recognised")
	}
}

func TestImageProblemsFlagsEachDeparture(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"builder without $BUILDPLATFORM", "FROM --platform=$BUILDPLATFORM golang:", "FROM golang:", "lacks --platform=$BUILDPLATFORM"},
		{"node builder without $BUILDPLATFORM", "FROM --platform=$BUILDPLATFORM node:", "FROM node:", "lacks --platform=$BUILDPLATFORM"},
		{"a pinned build platform", "--platform=$BUILDPLATFORM golang:", "--platform=linux/amd64 golang:", "lacks --platform=$BUILDPLATFORM"},
		{"final stage on a distribution", "FROM scratch", "FROM alpine:3", "want scratch or distroless static"},
		{"final stage on debian", "FROM scratch", "FROM debian@sha256:cc", "want scratch or distroless static"},
		{"root user", "USER 65532:65532", "USER 0", "runs as root"},
		{"root by name", "USER 65532:65532", "USER root:root", "runs as root"},
		{"no user", "USER 65532:65532\n", "", "sets no USER"},
		{"a RUN in the final stage", "USER 65532:65532", "RUN chmod +x /board\nUSER 65532:65532", "final stage has a RUN"},
		{"no CA certificates", "COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt\n", "", "CA certificates"},
		{"build for the host", "GOARCH=$TARGETARCH", "GOARCH=amd64", "GOOS=$TARGETOS GOARCH=$TARGETARCH"},
		{"no TARGETOS", "GOOS=$TARGETOS ", "", "GOOS=$TARGETOS GOARCH=$TARGETARCH"},
	}
	for _, tc := range cases {
		mutated := strings.Replace(goodDockerfile, tc.from, tc.to, 1)
		if mutated == goodDockerfile {
			t.Fatalf("%s: the mutation changed nothing", tc.name)
		}
		got := strings.Join(imageProblems(mutated), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: problems %q, want one containing %q", tc.name, got, tc.want)
		}
	}
	if got := imageProblems("FROM scratch\nUSER 1\n"); len(got) != 1 || !strings.Contains(got[0], "not multi-stage") {
		t.Errorf("single-stage Dockerfile: %q", got)
	}
}

func TestCarriesTimezonesNeedsEmbeddedOrCopiedZoneinfo(t *testing.T) {
	without := strings.Replace(goodDockerfile, "-tags timetzdata ", "", 1)
	if carriesTimezones(without) {
		t.Error("a Dockerfile with no tzdata passed")
	}
	copied := strings.Replace(without, "USER 65532", "COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo\nUSER 65532", 1)
	if !carriesTimezones(copied) {
		t.Error("zoneinfo copied into the final stage was not recognised")
	}
	if carriesTimezones("") {
		t.Error("an empty Dockerfile passed")
	}
}

// TestRootDockerfileBuildsTheMultiArchContainer: SPEC 12.4 as built.
func TestRootDockerfileBuildsTheMultiArchContainer(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")
	for _, problem := range imageProblems(dockerfile) {
		t.Errorf("Dockerfile: %s", problem)
	}
	if !carriesTimezones(dockerfile) {
		t.Error("Dockerfile: the image carries no tzdata (embed it with -tags timetzdata or COPY zoneinfo)")
	}
}

// TestImageCiStepBuildsBothPlatformsAndPushesNothing: the Quality stage's
// image step builds linux/amd64 and linux/arm64 from the digest-pinned buildx
// and BuildKit images, loads the amd64 image, runs `board version`, probes
// /healthz and inspects both manifests, and never pushes.
func TestImageCiStepBuildsBothPlatformsAndPushesNothing(t *testing.T) {
	build := readRepoFile(t, "ci", "image-build.sh")
	check := readRepoFile(t, "ci", "image-check.sh")
	for _, want := range []string{
		"--platform linux/amd64,linux/arm64",
		"--platform linux/amd64",
		"--load",
		"docker save",
		"type=oci,dest=",
		"docker/buildx-bin:", "moby/buildkit:",
	} {
		if !strings.Contains(build, want) {
			t.Errorf("ci/image-build.sh lacks %q", want)
		}
	}
	for _, image := range []string{"buildx_image=", "buildkit_image="} {
		for _, line := range strings.Split(build, "\n") {
			if strings.HasPrefix(line, image) && !strings.Contains(line, "@sha256:") {
				t.Errorf("ci/image-build.sh: %s is not pinned by digest", image)
			}
		}
	}
	for _, want := range []string{"go run ./ci/imagecheck", `docker run --rm "$tag" version`, "/healthz"} {
		if !strings.Contains(check, want) {
			t.Errorf("ci/image-check.sh lacks %q", want)
		}
	}
	for name, script := range map[string]string{"ci/image-build.sh": build, "ci/image-check.sh": check} {
		for _, line := range strings.Split(script, "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "#") {
				continue
			}
			if strings.Contains(code, "docker push") || strings.Contains(code, "--push") || strings.Contains(code, "type=registry") {
				t.Errorf("%s pushes: %q", name, code)
			}
		}
	}
	jf := readRepoFile(t, "Jenkinsfile")
	expectProblems(t, jf, imageUnitProblems, map[string]string{
		"an image checked before it is built": replaceOnce(t, jf, `ci/image-build.sh "$BOARD_CI_OUT/board-image.tar" && ci/image-check.sh "$BOARD_CI_OUT"`, `ci/image-check.sh "$BOARD_CI_OUT" && ci/image-build.sh "$BOARD_CI_OUT/board-image.tar"`),
		"an image that is never checked":      replaceOnce(t, jf, ` && ci/image-check.sh "$BOARD_CI_OUT"`, ""),
		"a pushed image":                      replaceOnce(t, jf, `ci/image-check.sh "$BOARD_CI_OUT"'`, `ci/image-check.sh "$BOARD_CI_OUT" && docker push board:ci'`),
		"a pushing build":                     replaceOnce(t, jf, `ci/image-build.sh "$BOARD_CI_OUT/board-image.tar" &&`, `ci/image-build.sh --push "$BOARD_CI_OUT/board-image.tar" &&`),
		"a tar nobody can scan":               replaceOnce(t, jf, "includes: 'ci-results/board-image.tar,ci-results/board-image-oci.tar'", "includes: 'ci-results/none.tar'"),
		"an image unit with another limit":    replaceOnce(t, jf, "units << [name: 'Image', minutes: 30,", "units << [name: 'Image', minutes: 5,"),
	})
}

// imageUnitProblems: the Core phase's Image unit builds the image (both
// platforms, never pushed), checks it after that and stashes the tar for Quality
// (Quality's need for that stash is qualityProblems's).
func imageUnitProblems(jf string) []string {
	var problems []string
	core, _ := jenkinsStage(jf, "Core")
	i := strings.Index(core, "units << [name: 'Image'")
	if i < 0 {
		return []string{"Core has no Image unit"}
	}
	unit, _, _ := strings.Cut(core[i:], "units << [name: 'Allure smoke'")
	build, check := strings.Index(unit, "ci/image-build.sh"), strings.Index(unit, "ci/image-check.sh")
	if build < 0 || check < 0 || build > check {
		problems = append(problems, "the Image unit does not build the image and then check it")
	}
	if !strings.Contains(unit, "minutes: 30,") {
		problems = append(problems, "the Image unit's limit is not 30 minutes")
	}
	if !strings.Contains(unit, "includes: 'ci-results/board-image.tar,ci-results/board-image-oci.tar'") {
		problems = append(problems, "the Image unit does not stash the image tars")
	}
	for _, l := range jenkinsfileLines(jf) {
		for _, bad := range []string{"docker push", "--push", "type=registry"} {
			if strings.Contains(l, bad) {
				problems = append(problems, "the Jenkinsfile pushes the image: "+l)
			}
		}
	}
	return problems
}
