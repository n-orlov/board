// Package version reports the version the binary was built as, read from the
// Go build info the toolchain embeds. There is no hand-maintained version
// constant and no linker flag: a tagged module build reports its tag, a
// checkout build reports devel plus the short commit, and a build from a
// modified tree appends -dirty.
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

// readBuildInfo is the build-info source. Tests replace it.
var readBuildInfo = debug.ReadBuildInfo

// Current returns the version of the running binary, read from build info.
func Current() string {
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
	setting := func(key string) string {
		for _, s := range info.Settings {
			if s.Key == key {
				return s.Value
			}
		}
		return ""
	}

	base := info.Main.Version
	dirty := setting("vcs.modified") == "true"
	if strings.HasSuffix(base, "+dirty") {
		dirty = true
		base = strings.TrimSuffix(base, "+dirty")
	}
	if base == "" || base == "(devel)" {
		base = develVersion
		if rev := setting("vcs.revision"); rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			base += "+" + rev
		}
	}
	if dirty {
		base += DirtySuffix
	}
	return base
}
