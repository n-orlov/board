package api

import (
	"bytes"
	"os"
	"testing"
)

func TestYAMLIsTheCheckedInDocument(t *testing.T) {
	want, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(YAML(), want) {
		t.Fatal("the embedded document differs from api/openapi.yaml")
	}
}

func TestJSONHoldsTheSameOperationsAsTheYAML(t *testing.T) {
	doc, err := JSON()
	if err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", doc["openapi"])
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths is %T, want a mapping", doc["paths"])
	}
	for _, p := range []string{"/healthz", "/readyz", "/api/openapi.yaml", "/api/openapi.json", "/api/docs"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("the JSON document has no path %s", p)
		}
	}
}

func TestParseRefusesABrokenOrEmptyDocument(t *testing.T) {
	for name, raw := range map[string][]byte{"not a mapping": []byte("- a\n- b\n"), "empty": nil} {
		if _, err := parse(raw); err == nil {
			t.Errorf("%s: parse succeeded, want an error", name)
		}
	}
}
