package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// findModule walks up from dir for go.mod and returns the module root and
// module path.
func findModule(dir string) (root, modulePath string, err error) {
	for {
		data, readErr := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // G304: go.mod is looked up in the directories above the directory the tool was pointed at
		if readErr == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					return dir, strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module")), `"`), nil
				}
			}
			return "", "", fmt.Errorf("%s has no module line", filepath.Join(dir, "go.mod"))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

// skipDir reports whether a directory is never part of the module's
// package set: hidden and underscore dirs, vendor, testdata, and the
// suite's own output directory.
func skipDir(name string) bool {
	switch name {
	case "vendor", "testdata", "ci-results", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// skipsTree reports whether the directory p (named name) is outside the
// package set: a skipped name, or the root of a nested module.
func skipsTree(p, name string) bool {
	if skipDir(name) {
		return true
	}
	_, statErr := os.Stat(filepath.Join(p, "go.mod"))
	return statErr == nil
}

// isSourceFile reports whether name is a non-test Go file.
func isSourceFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// sourcePackages returns every directory (slash-separated, relative to the
// module root, "." for the root) holding at least one non-test .go file.
// Nested modules are not part of this module's package set.
func sourcePackages(root string) (map[string]bool, error) {
	pkgs := make(map[string]bool)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if p != root && skipsTree(p, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isSourceFile(d.Name()) {
			rel, relErr := filepath.Rel(root, filepath.Dir(p))
			if relErr != nil {
				return relErr
			}
			pkgs[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	return pkgs, err
}

// packageOf maps a profile file name ("<module>/<dir>/<file>.go") to the
// package directory relative to the module root.
func packageOf(file, modulePath string) (string, error) {
	rel := strings.TrimPrefix(file, modulePath+"/")
	if rel == file {
		return "", fmt.Errorf("profile file %q is not under module %q", file, modulePath)
	}
	return path.Dir(rel), nil
}
