// Command imagecheck inspects the multi-platform OCI layout that
// ci/image-build.sh writes (SPEC 12.4): a manifest exists for linux/amd64 and
// linux/arm64, each runs as a non-root user with /vh as its entrypoint and the title
// "Village Hall", is 30 MB or less, and the /vh in each is an ELF binary for that platform's architecture.
//
//	go run ./ci/imagecheck ci-results/board-image-oci.tar
//
// It prints one line per platform and exits 1 on any shortfall.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		say(stderr, "usage: imagecheck <oci-layout.tar>\n")
		return 2
	}
	data, err := os.ReadFile(args[0]) //nolint:gosec // G304, G703: the layout path is the tool's own command-line argument
	if err != nil {
		say(stderr, "imagecheck: %v\n", err)
		return 1
	}
	images, err := Inspect(bytes.NewReader(data))
	if err != nil {
		say(stderr, "imagecheck: %v\n", err)
		return 1
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Platform < images[j].Platform })
	for _, image := range images {
		say(stdout, "%s user=%s entrypoint=%v title=%q size=%d binary=%s\n", image.Platform, image.User, image.Entrypoint, image.Title, image.Size, image.Machine)
	}
	problems := Verify(images)
	for _, problem := range problems {
		say(stderr, "imagecheck: %s\n", problem)
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

// say writes to a report stream; a failed write to stdout or stderr has nowhere
// better to be reported.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
