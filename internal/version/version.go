// Package version reports the version and the revision the binary was built as.
// There is no hand-maintained version constant. The container image build
// stamps both with the linker (-X github.com/n-orlov/village-hall/internal/version.version
// and .revision, from the Dockerfile's VERSION and REVISION build args); a build
// without them reads the Go build info the toolchain embeds: a tagged module build
// reports its tag, a checkout build reports devel plus the short commit, and a
// build from a modified tree appends -dirty. A build with neither reports devel.
package version

import (
	"runtime/debug"
	"strings"
)

// DirtySuffix is appended to the version of a build made from a modified tree.
const DirtySuffix = "-dirty"

// develVersion is what a build with no module version (a plain checkout build)
// reports before the commit and the dirty marker.
const develVersion = "devel"

// version and revision are set by the linker (-ldflags "-X ...version=v1.2.3
// -X ...revision=<commit>"); empty means the build did not stamp them.
var (
	version  string
	revision string
)

// readBuildInfo is the build-info source. Tests replace it.
var readBuildInfo = debug.ReadBuildInfo

// Current returns the version of the running binary: the linker-stamped
// version when there is one, otherwise the version derived from build info.
func Current() string {
	if version != "" {
		return version
	}
	info, ok := readBuildInfo()
	if !ok {
		return develVersion
	}
	return Stamp(info)
}

// Stamp derives the version string from build info.
//
// The base is the main module version when it is a real one (a tag or a
// pseudo-version), otherwise "devel" followed by "+<short commit>" when the
// build recorded a commit. A modified tree is detected from the vcs.modified
// build setting, or from the "+dirty" marker the toolchain itself puts on the
// main module version, and always renders as the trailing "-dirty".
func Stamp(info *debug.BuildInfo) string {
	if info == nil {
		return develVersion
	}
	base, dirty := info.Main.Version, setting(info, "vcs.modified") == "true"
	if strings.HasSuffix(base, "+dirty") {
		dirty = true
		base = strings.TrimSuffix(base, "+dirty")
	}
	if base == "" || base == "(devel)" {
		base = develBase(setting(info, "vcs.revision"))
	}
	if dirty {
		base += DirtySuffix
	}
	return base
}

// setting returns the named build setting, or "" when the build did not record it.
func setting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// develBase is "devel", followed by "+<first 12 characters of rev>" when a
// commit was recorded.
func develBase(rev string) string {
	if rev == "" {
		return develVersion
	}
	return develVersion + "+" + rev[:min(len(rev), 12)]
}

// Revision returns the commit the running binary was built from: the
// linker-stamped revision when there is one, otherwise the vcs.revision build
// setting, otherwise "" (a build that recorded neither).
func Revision() string {
	if revision != "" {
		return revision
	}
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return ""
	}
	return setting(info, "vcs.revision")
}

// Line is what `board version` prints: the version, then a space and the
// revision when there is one the version does not already carry (a checkout
// build's devel+<short commit> names its commit itself).
func Line() string {
	v, rev := Current(), Revision()
	if rev == "" || strings.Contains(v, rev[:min(len(rev), 12)]) {
		return v
	}
	return v + " " + rev
}
