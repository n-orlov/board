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
