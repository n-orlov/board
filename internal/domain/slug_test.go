package domain

import (
	"strings"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name string
		slug string
		ok   bool
	}{
		{"length 1 is too short", "a", false},
		{"length 2 is the minimum", "ab", true},
		{"length 40 is the maximum", strings.Repeat("a", 40), true},
		{"length 41 is too long", strings.Repeat("a", 41), false},
		{"uppercase is rejected", "Abc", false},
		{"digits and hyphens are allowed", "a1-2b", true},
		{"empty is rejected", "", false},
		{"underscore is rejected", "a_b", false},
		{"space is rejected", "a b", false},
		{"trailing newline is rejected", "ab\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSlug(tc.slug)
			if (err == nil) != tc.ok {
				t.Fatalf("ValidateSlug(%q) = %v, want ok=%v", tc.slug, err, tc.ok)
			}
		})
	}
}
