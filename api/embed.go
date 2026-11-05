// Package api embeds the OpenAPI document, the source of truth for the HTTP
// API, so the binary can serve it.
package api

import (
	_ "embed" // the document is embedded, not read at run time
	"errors"
	"fmt"
	"sync"

	"go.yaml.in/yaml/v3"
)

//go:embed openapi.yaml
var yamlDocument []byte

// YAML returns the OpenAPI document exactly as checked in.
func YAML() []byte { return yamlDocument }

// JSON returns the OpenAPI document as a value that marshals to the same
// content as YAML. It fails when the embedded document is not a YAML mapping.
func JSON() (map[string]any, error) { return jsonDocument() }

var jsonDocument = sync.OnceValues(func() (map[string]any, error) { return parse(yamlDocument) })

// parse reads a YAML mapping.
func parse(raw []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse the OpenAPI document: %w", err)
	}
	if len(doc) == 0 {
		return nil, errors.New("the OpenAPI document is empty")
	}
	return doc, nil
}
