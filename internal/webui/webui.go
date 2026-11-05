// Package webui embeds the built Svelte app (web/, built into dist/ by
// `npm run build` in web/) so the binary serves it with no files beside it.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed fallback
var fallback embed.FS

// Files returns the embedded app, rooted at its index.html. A checkout whose web/
// has not been built carries only a placeholder page, so the package (and every Go
// test) still compiles and serves something; Built reports which one this is.
func Files() fs.FS {
	if Built() {
		return sub(dist, "dist")
	}
	return sub(fallback, "fallback")
}

// Built reports whether the real web build is embedded.
func Built() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}

func sub(fsys fs.FS, dir string) fs.FS {
	s, err := fs.Sub(fsys, dir)
	if err != nil { // dir is a constant that the embed directive guarantees
		panic(err)
	}
	return s
}
