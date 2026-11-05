package webui

import (
	"io/fs"
	"strings"
	"testing"
)

func TestFilesHoldAnIndexWithTheAppMountPoint(t *testing.T) {
	b, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatalf("no index.html in the embedded UI: %v", err)
	}
	if !strings.Contains(string(b), `<div id="app">`) {
		t.Errorf("index.html has no app mount point: %q", b)
	}
}

func TestBuiltMatchesWhichTreeIsServed(t *testing.T) {
	_, err := fs.Stat(dist, "dist/index.html")
	if got, want := Built(), err == nil; got != want {
		t.Errorf("Built() = %v, but dist/index.html present = %v", got, want)
	}
	b, err := fs.ReadFile(Files(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	// The placeholder page says so; a real build never does.
	if placeholder := strings.Contains(string(b), "was not built"); placeholder == Built() {
		t.Errorf("Built() = %v but index.html placeholder = %v", Built(), placeholder)
	}
}

// The placeholder is served when there is no build, and the build tree is served
// when there is one, whichever of the two this checkout embeds.
func TestFilesServeThePlaceholderOrTheBuildAsTold(t *testing.T) {
	b, err := fs.ReadFile(files(false), "index.html")
	if err != nil || !strings.Contains(string(b), "was not built") {
		t.Errorf("files(false): index.html = %q, %v, want the placeholder page", b, err)
	}
	if _, err := fs.Stat(files(true), "."); err != nil {
		t.Errorf("files(true): the build tree is not readable: %v", err)
	}
	if Built() {
		if b, err := fs.ReadFile(files(true), "index.html"); err != nil || strings.Contains(string(b), "was not built") {
			t.Errorf("files(true) with a build: index.html = %q, %v, want the built page", b, err)
		}
	}
}
