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
