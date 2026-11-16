// Package main implements ci/installcheck, the R9 agreement check behind
// ci/lint.sh:
//
//	go run ./ci/installcheck [install.sh [INSTALL-AGENT.md]]
//
// It reads install.sh and INSTALL-AGENT.md and fails (exit 1, one line per
// disagreement) when they differ on the asset name pattern, the OS list,
// the arch list, the checksum file name, the default install directory or
// the set of environment variable names. Exit 2 is a usage or input error:
// a file cannot be read, or one of them lacks a fact the check needs.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	paths := []string{"install.sh", "INSTALL-AGENT.md"}
	if len(args) > 2 {
		_, _ = fmt.Fprintln(stderr, "usage: installcheck [install.sh [INSTALL-AGENT.md]]")
		return 2
	}
	copy(paths, args)
	script, err := os.ReadFile(paths[0])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "installcheck:", err)
		return 2
	}
	guide, err := os.ReadFile(paths[1])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "installcheck:", err)
		return 2
	}
	problems, err := Check(string(script), string(guide))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "installcheck:", err)
		return 2
	}
	for _, p := range problems {
		_, _ = fmt.Fprintf(stderr, "installcheck: %s and %s disagree: %s\n", paths[0], paths[1], p)
	}
	if len(problems) > 0 {
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "installcheck: %s and %s agree on the asset name, OS list, arch list, checksum file, install directory and variable names\n", paths[0], paths[1])
	return 0
}
