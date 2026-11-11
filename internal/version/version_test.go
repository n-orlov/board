package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func info(mainVersion string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}, Settings: settings}
}

func TestStampAppendsDirtyForModifiedTree(t *testing.T) {
	got := Stamp(info("v1.2.3", debug.BuildSetting{Key: "vcs.modified", Value: "true"}))
	if got != "v1.2.3-dirty" {
		t.Fatalf("modified tree: got %q, want v1.2.3-dirty", got)
	}
}

func TestStampCleanTreeHasNoDirtySuffix(t *testing.T) {
	got := Stamp(info("v1.2.3", debug.BuildSetting{Key: "vcs.modified", Value: "false"}))
	if got != "v1.2.3" {
		t.Fatalf("clean tree: got %q, want v1.2.3", got)
	}
}

func TestStampNormalisesToolchainDirtyMarker(t *testing.T) {
	got := Stamp(info("v0.0.0-20260101000000-abcdef123456+dirty"))
	if got != "v0.0.0-20260101000000-abcdef123456-dirty" {
		t.Fatalf("toolchain +dirty marker: got %q", got)
	}
}

func TestStampDevelBuildUsesShortCommit(t *testing.T) {
	got := Stamp(info("(devel)",
		debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		debug.BuildSetting{Key: "vcs.modified", Value: "true"}))
	if got != "devel+0123456789ab-dirty" {
		t.Fatalf("devel build: got %q", got)
	}
}

func TestStampWithoutVersionOrCommit(t *testing.T) {
	if got := Stamp(info("")); got != "devel" {
		t.Fatalf("no version info: got %q, want devel", got)
	}
	if got := Stamp(nil); got != "devel" {
		t.Fatalf("nil info: got %q, want devel", got)
	}
}

func TestCurrentReadsBuildInfoSource(t *testing.T) {
	orig := readBuildInfo
	defer func() { readBuildInfo = orig }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return info("v9.8.7", debug.BuildSetting{Key: "vcs.modified", Value: "true"}), true
	}
	if got := Current(); got != "v9.8.7-dirty" {
		t.Fatalf("Current must come from build info: got %q", got)
	}

	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	if got := Current(); got != "devel" {
		t.Fatalf("Current without build info: got %q, want devel", got)
	}
}

func TestCurrentOfTestBinaryIsOneLine(t *testing.T) {
	got := Current()
	if got == "" || strings.ContainsAny(got, " \n") {
		t.Fatalf("Current() = %q, want a single token", got)
	}
}

// withLinkerValues sets the linker-stamped variables for one test.
func withLinkerValues(t *testing.T, v, r string) {
	t.Helper()
	origV, origR := version, revision
	version, revision = v, r
	t.Cleanup(func() { version, revision = origV, origR })
}

func withBuildInfo(t *testing.T, bi *debug.BuildInfo, ok bool) {
	t.Helper()
	orig := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return bi, ok }
	t.Cleanup(func() { readBuildInfo = orig })
}

func TestLinkerValuesWinOverBuildInfo(t *testing.T) {
	withLinkerValues(t, "v2.0.0", "feedface00112233445566778899aabbccddeeff")
	withBuildInfo(t, info("v9.8.7",
		debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		debug.BuildSetting{Key: "vcs.modified", Value: "true"}), true)
	if got := Current(); got != "v2.0.0" {
		t.Fatalf("Current() = %q, want the linker version v2.0.0", got)
	}
	if got := Revision(); got != "feedface00112233445566778899aabbccddeeff" {
		t.Fatalf("Revision() = %q, want the linker revision", got)
	}
}

func TestEachLinkerValueIsIndependent(t *testing.T) {
	bi := info("(devel)", debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"})
	withBuildInfo(t, bi, true)

	withLinkerValues(t, "v2.0.0", "")
	if Current() != "v2.0.0" || Revision() != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("version only: Current=%q Revision=%q", Current(), Revision())
	}
	withLinkerValues(t, "", "cafe")
	if Current() != "devel+0123456789ab" || Revision() != "cafe" {
		t.Fatalf("revision only: Current=%q Revision=%q", Current(), Revision())
	}
}

func TestBuildWithNeitherArgsNorVCSInfoReportsDevel(t *testing.T) {
	withLinkerValues(t, "", "")
	withBuildInfo(t, nil, false)
	if got := Current(); got != "devel" {
		t.Fatalf("Current() = %q, want devel", got)
	}
	if got := Revision(); got != "" {
		t.Fatalf("Revision() = %q, want empty", got)
	}
	withBuildInfo(t, info(""), true)
	if got, rev := Current(), Revision(); got != "devel" || rev != "" {
		t.Fatalf("empty build info: Current=%q Revision=%q", got, rev)
	}
	withBuildInfo(t, nil, true)
	if got := Revision(); got != "" {
		t.Fatalf("nil build info: Revision() = %q, want empty", got)
	}
}

func TestRevisionFallsBackToVCSRevision(t *testing.T) {
	withLinkerValues(t, "", "")
	withBuildInfo(t, info("(devel)", debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"}), true)
	if got := Revision(); got != "0123456789abcdef" {
		t.Fatalf("Revision() = %q, want the vcs.revision setting", got)
	}
}

func TestLineAppendsTheRevisionOnlyWhenTheVersionDoesNotCarryIt(t *testing.T) {
	withBuildInfo(t, info("(devel)"), true)
	withLinkerValues(t, "v2.0.0", "feedface00112233")
	if got := Line(); got != "v2.0.0 feedface00112233" {
		t.Fatalf("stamped build: Line() = %q", got)
	}
	withLinkerValues(t, "v2.0.0", "")
	if got := Line(); got != "v2.0.0" {
		t.Fatalf("no revision: Line() = %q", got)
	}
	withLinkerValues(t, "", "")
	withBuildInfo(t, info("(devel)", debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"}), true)
	if got := Line(); got != "devel+0123456789ab" {
		t.Fatalf("checkout build already names its commit: Line() = %q", got)
	}
	withLinkerValues(t, "abcdef0", "abcdef0123456789")
	if got := Line(); got != "abcdef0 abcdef0123456789" {
		t.Fatalf("short-sha version: Line() = %q", got)
	}
}
