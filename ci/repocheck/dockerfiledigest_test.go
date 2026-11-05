// dockerfiledigest_test.go: every external image in ci/Dockerfile (each FROM
// and each COPY --from naming an image) is pinned by sha256 digest, so a
// re-pushed tag can never change what the CI image is built from.
package repocheck

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var digestRE = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)

// undigestedFroms returns each FROM line of a Dockerfile whose image is
// neither pinned by digest, `scratch`, nor an earlier stage's name.
func undigestedFroms(dockerfile string) []string {
	var bad []string
	stages := map[string]bool{"scratch": true}
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		args := fields[1:]
		for len(args) > 0 && strings.HasPrefix(args[0], "--") {
			args = args[1:] // --platform=...
		}
		if len(args) == 0 {
			bad = append(bad, strings.TrimSpace(line))
			continue
		}
		image := args[0]
		if !stages[strings.ToLower(image)] && !digestRE.MatchString(image) {
			bad = append(bad, strings.TrimSpace(line))
		}
		if len(args) >= 3 && strings.EqualFold(args[1], "AS") {
			stages[strings.ToLower(args[2])] = true
		}
	}
	return bad
}

// undigestedCopyFroms returns each COPY line whose --from names an image
// that is neither pinned by digest nor an earlier stage's name.
func undigestedCopyFroms(dockerfile string) []string {
	var bad []string
	stages := map[string]bool{}
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.EqualFold(fields[0], "FROM") && strings.EqualFold(fields[len(fields)-2], "AS") {
			stages[strings.ToLower(fields[len(fields)-1])] = true
		}
		if len(fields) < 2 || !strings.EqualFold(fields[0], "COPY") {
			continue
		}
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "--from=") {
				continue
			}
			src := strings.TrimPrefix(f, "--from=")
			if !stages[strings.ToLower(src)] && !digestRE.MatchString(src) {
				bad = append(bad, strings.TrimSpace(line))
			}
		}
	}
	return bad
}

func TestUndigestedFromsFlagsAFromWithoutADigest(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	cases := []struct {
		name, dockerfile string
		want             int
	}{
		{"tag only", "FROM golang:1.26.4-trixie\n", 1},
		{"tag only with stage name", "FROM golang:1.26.4-trixie AS tools\n", 1},
		{"untagged", "FROM golang\n", 1},
		{"short digest", "FROM golang:1@sha256:abc\n", 1},
		{"platform flag does not hide it", "FROM --platform=linux/amd64 golang:1\n", 1},
		{"lowercase instruction", "from golang:1\n", 1},
		{"one digested one not", "FROM golang:1" + digest + " AS a\nFROM golang:2\n", 1},
		{"digested", "FROM golang:1" + digest + "\n", 0},
		{"digested with stage name", "FROM golang:1" + digest + " AS tools\n", 0},
		{"digest without tag", "FROM golang" + digest + "\n", 0},
		{"scratch and earlier stage", "FROM golang:1" + digest + " AS a\nFROM a\nFROM scratch\n", 0},
		{"COPY --from is not a FROM line", "FROM golang:1" + digest + "\nCOPY --from=x /a /b\n", 0},
	}
	for _, tc := range cases {
		if got := undigestedFroms(tc.dockerfile); len(got) != tc.want {
			t.Errorf("%s: flagged %q, want %d line(s)", tc.name, got, tc.want)
		}
	}
}

func TestUndigestedCopyFromsFlagsAnImageWithoutADigest(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("b", 64)
	cases := []struct {
		name, dockerfile string
		want             int
	}{
		{"tag only", "FROM scratch\nCOPY --from=trivy:0.72.0 /a /b\n", 1},
		{"registry tag only", "FROM scratch\nCOPY --from=public.ecr.aws/aquasecurity/trivy:0.72.0 /a /b\n", 1},
		{"digested", "FROM scratch\nCOPY --from=trivy" + digest + " /a /b\n", 0},
		{"earlier stage", "FROM scratch AS tools\nCOPY --from=tools /a /b\n", 0},
		{"unknown stage", "FROM scratch AS tools\nCOPY --from=other /a /b\n", 1},
		{"plain copy", "FROM scratch\nCOPY a b\n", 0},
	}
	for _, tc := range cases {
		if got := undigestedCopyFroms(tc.dockerfile); len(got) != tc.want {
			t.Errorf("%s: flagged %q, want %d line(s)", tc.name, got, tc.want)
		}
	}
}

// TestEveryDockerfilePinsEveryImageByDigest covers both Dockerfiles in the
// tree: the CI toolchain image and the root Dockerfile that builds the
// container image.
func TestEveryDockerfilePinsEveryImageByDigest(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	for _, rel := range []string{"Dockerfile", filepath.Join("ci", "Dockerfile")} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if bad := undigestedFroms(string(raw)); len(bad) > 0 {
			t.Errorf("%s FROM lines without an @sha256 digest: %q", rel, bad)
		}
		if bad := undigestedCopyFroms(string(raw)); len(bad) > 0 {
			t.Errorf("%s COPY --from lines without an @sha256 digest: %q", rel, bad)
		}
	}
}

// repositoryRoot walks up from the test's working directory to the directory
// holding go.mod.
func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("locate repository root containing go.mod")
		}
		directory = parent
	}
}
