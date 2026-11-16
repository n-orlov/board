package repocheck

// ci/release-build.sh builds the release assets of SPEC 12.5 and ci/release-check.sh
// is its dry run in the Image unit. The assertions on what a build produces are
// releasebuild_test.go's (behind the releasebuild tag, run by ci/release-check.sh);
// these read the scripts and the Jenkinsfile, so a lost flag or a lost step fails
// the ordinary Go tier too.

import (
	"strings"
	"testing"
)

func TestReleaseBuildScriptBuildsTheAssetsAndPushesNothing(t *testing.T) {
	build := readRepoFile(t, "ci", "release-build.sh")
	for _, want := range []string{
		"CGO_ENABLED=0", "-trimpath",
		"internal/version.version=$version", "internal/version.revision=$revision",
		"linux_amd64 linux_arm64 darwin_amd64 darwin_arm64",
		"vh_$target.tar.gz", "vh_$target.cdx.json", "village-hall-image.cdx.json",
		"--format cyclonedx",
		`BOARD_IMAGE_NAME="village-hall:$version"`, "ci/image-build.sh",
		"install.sh INSTALL-AGENT.md", "sha256sum", "checksums.txt",
	} {
		if !strings.Contains(build, want) {
			t.Errorf("ci/release-build.sh lacks %q", want)
		}
	}
	for name, script := range map[string]string{"ci/release-build.sh": build, "ci/release-check.sh": readRepoFile(t, "ci", "release-check.sh")} {
		for _, line := range strings.Split(script, "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "#") {
				continue
			}
			for _, bad := range []string{"docker push", "--push", "type=registry", "gh release", "git tag", "git push"} {
				if strings.Contains(code, bad) {
					t.Errorf("%s publishes: %q", name, code)
				}
			}
		}
	}
}

func TestImageBuildKeepsBoardCiUnlessTheReleaseRenamesIt(t *testing.T) {
	build := readRepoFile(t, "ci", "image-build.sh")
	for _, want := range []string{"tag=${BOARD_IMAGE_TAG:-board:ci}", `,name=$oci_name`} {
		if !strings.Contains(build, want) {
			t.Errorf("ci/image-build.sh lacks %q", want)
		}
	}
}

func TestImageUnitRunsTheReleaseDryRunAfterTheImageCheck(t *testing.T) {
	core, _ := jenkinsStage(readRepoFile(t, "Jenkinsfile"), "Core")
	start, _, _ := strings.Cut(core, "units << [name: 'Allure smoke'")
	i := strings.Index(start, "units << [name: 'Image'")
	if i < 0 {
		t.Fatal("Core has no Image unit")
	}
	unit := start[i:]
	check, dry := strings.Index(unit, "ci/image-check.sh"), strings.Index(unit, `ci/release-check.sh "$BOARD_CI_OUT"`)
	if check < 0 || dry < check {
		t.Error("the Image unit does not run ci/release-check.sh after ci/image-check.sh")
	}
	check = strings.Index(readRepoFile(t, "ci", "release-check.sh"), "-tags releasebuild")
	if check < 0 {
		t.Error("ci/release-check.sh does not run the releasebuild tests")
	}
}
