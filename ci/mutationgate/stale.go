package main

// Incremental mutation (SPEC 15.3, the Jenkinsfile's nightly): the build
// reuses the gremlins reports of the last full run (its reference) for every
// unit whose report could not have changed since, and runs gremlins over the
// rest. -stale <ref> lists the rest. A unit is stale when a file changed
// since ref (added, changed or deleted) lies in its package's directory or
// below it, tests and testdata included, or is a non-test file (not
// _test.go, not under testdata/) in or below the directory of a module
// package that its package or its tests import, directly or not (go list
// -deps -test): a test a dependency's change breaks can stop killing a
// mutant, while a dependency's own tests never run in this package's. A part
// of a split package goes stale with its package: any test of the package
// can kill any part's mutants.
//
// Some changes can alter every report: the module's dependencies, the CI
// image (Go and gremlins versions), the scripts that run gremlins, and the
// units themselves (mutation_go's packages, scopes and parts). Then -stale
// exits 3 and the build runs in full. The gate's own code and thresholds are
// not among them: a reused report is gremlins' raw output, judged again by
// this build's mutationgate.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// sharedInputs are the files whose change can alter every unit's report.
var sharedInputs = []string{"go.mod", "go.sum", "ci/Dockerfile", "ci/run.sh", "ci/mutation.sh"}

// fullRunError says why no report of the reference can be reused.
type fullRunError struct{ reason string }

func (e fullRunError) Error() string { return "every unit must run: " + e.reason }

// depsFunc returns the slash-separated directories, relative to the module
// root, of a package and of every module package it and its tests import.
type depsFunc func(root, pkg string) ([]string, error)

// staleUnits returns, in order, the units whose report made at ref may differ
// from one made in the working tree at root, or a fullRunError when a shared
// input changed. cfgFile is the config's slash path in the repository ("" if
// it lives outside it).
func staleUnits(cfg config, root, cfgFile, ref string, units []string, deps depsFunc) ([]string, error) {
	out, err := gitOutput(root, "diff", "--name-only", "--no-renames", ref)
	if err != nil {
		return nil, err
	}
	files := strings.Fields(string(out))
	if err := sharedChange(cfg, root, cfgFile, ref, files); err != nil {
		return nil, err
	}
	known := map[string][]string{}
	var stale []string
	for _, u := range units {
		pkg, _ := splitUnit(u)
		if _, ok := known[pkg]; !ok {
			if known[pkg], err = deps(root, pkg); err != nil {
				return nil, err
			}
		}
		if slices.ContainsFunc(files, func(f string) bool { return reaches(f, pkg, known[pkg]) }) {
			stale = append(stale, u)
		}
	}
	return stale, nil
}

// sharedChange returns a fullRunError when one of files is a shared input or
// the config's units differ from those at ref.
func sharedChange(cfg config, root, cfgFile, ref string, files []string) error {
	for _, f := range files {
		if slices.Contains(sharedInputs, f) {
			return fullRunError{f + " changed"}
		}
	}
	if cfgFile == "" || !slices.Contains(files, cfgFile) {
		return nil
	}
	return unitsChanged(cfg, root, cfgFile, ref)
}

// unitsChanged compares the config's packages, scopes and parts with the
// config at ref.
func unitsChanged(cfg config, root, cfgFile, ref string) error {
	data, err := gitOutput(root, "show", ref+":"+cfgFile)
	var all struct {
		MutationGo config `json:"mutation_go"`
	}
	if err == nil {
		err = json.Unmarshal(data, &all)
	}
	if err != nil {
		return fullRunError{cfgFile + " unreadable at " + ref}
	}
	old := all.MutationGo
	if !slices.Equal(old.Packages, cfg.Packages) || !reflect.DeepEqual(old.Scopes, cfg.Scopes) || !reflect.DeepEqual(old.Parts, cfg.Parts) {
		return fullRunError{"mutation_go's packages, scopes or parts changed in " + cfgFile}
	}
	return nil
}

// reaches reports whether a changed file f can change the report of a unit
// of package own, whose module dependencies (own included) lie in dirs.
func reaches(f, own string, dirs []string) bool {
	if below(f, own) {
		return true
	}
	if strings.HasSuffix(f, "_test.go") || strings.Contains("/"+f, "/testdata/") {
		return false
	}
	return slices.ContainsFunc(dirs, func(d string) bool { return below(f, d) })
}

// below reports whether f lies in directory d or below it (d "." holds only
// the module root's own files: its subdirectories are other packages).
func below(f, d string) bool {
	return strings.HasPrefix(f, d+"/") || (d == "." && path.Dir(f) == ".")
}

// goListDeps is the depsFunc of the real build: go list -deps -test, kept to
// the main module's packages.
func goListDeps(root, pkg string) ([]string, error) {
	cmd := exec.Command("go", "list", "-deps", "-test", "-f", "{{if .Module}}{{if .Module.Main}}{{.Dir}}{{end}}{{end}}", "./"+pkg) //nolint:gosec // G204: a package of the repository's own config
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps -test ./%s: %w", pkg, err)
	}
	var dirs []string
	for _, d := range strings.Fields(string(out)) {
		dirs = append(dirs, repoPath(root, d))
	}
	return dirs, nil
}

// repoPath returns p (absolute, or relative to root) as a slash path relative
// to root, "." for root itself, or "" when p is outside root.
func repoPath(root, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	if p == root {
		return "."
	}
	rel := strings.TrimPrefix(p, root+string(filepath.Separator))
	if rel == p {
		return ""
	}
	return filepath.ToSlash(rel)
}

// gitOutput runs git in root.
func gitOutput(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // G204: git with the ref the CI passes
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// printStale prints the stale units of units; exit 3 when every unit must run.
func printStale(cfg config, cfgPath, ref string, units []string, stdout, stderr io.Writer) int {
	root, err := os.Getwd()
	var stale []string
	if err == nil {
		stale, err = staleUnits(cfg, root, repoPath(root, cfgPath), ref, units, goListDeps)
	}
	var full fullRunError
	if errors.As(err, &full) {
		_, _ = fmt.Fprintln(stderr, "mutationgate: full:", full.reason)
		return 3
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "mutationgate:", err)
		return 2
	}
	printLines(stdout, stale)
	return 0
}
