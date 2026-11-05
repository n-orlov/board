package domain

import (
	"fmt"
	"regexp"
)

// SlugPattern is the grammar of an initiative slug: 2 to 40 characters of
// lowercase letters, digits and hyphens.
const SlugPattern = `^[a-z0-9-]{2,40}$`

var slugRE = regexp.MustCompile(SlugPattern)

// ValidateSlug returns nil when s is a valid slug.
func ValidateSlug(s string) error {
	if !slugRE.MatchString(s) {
		return fmt.Errorf("slug %q must match [a-z0-9-]{2,40}", s)
	}
	return nil
}
