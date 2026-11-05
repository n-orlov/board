package swaggerui

import (
	"regexp"
	"testing"
)

var assetRef = regexp.MustCompile(`(?:src|href)="([^"]+)"`)

func TestEveryAssetThePageReferencesIsEmbedded(t *testing.T) {
	refs := assetRef.FindAllStringSubmatch(Page, -1)
	if len(refs) != 3 {
		t.Fatalf("the page references %d assets, want 3: %q", len(refs), Page)
	}
	for _, ref := range refs {
		const prefix = "/api/docs/assets/"
		url := ref[1]
		if len(url) <= len(prefix) || url[:len(prefix)] != prefix {
			t.Errorf("%q is not under %s", url, prefix)
			continue
		}
		body, _, ok := Asset(url[len(prefix):])
		if !ok || len(body) == 0 {
			t.Errorf("%q is not an embedded asset", url)
		}
	}
}

func TestAssetContentTypesAndRefusals(t *testing.T) {
	for name, want := range map[string]string{
		"swagger-ui-bundle.js": "text/javascript",
		"init.js":              "text/javascript",
		"swagger-ui.css":       "text/css",
	} {
		if _, got, ok := Asset(name); !ok || got != want {
			t.Errorf("Asset(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"nope.js", "../swaggerui.go", "sub/init.js", "", "assets", "LICENSE"} {
		if _, _, ok := Asset(name); ok {
			t.Errorf("Asset(%q) served", name)
		}
	}
}
