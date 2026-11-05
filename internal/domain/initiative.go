package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of an initiative's fields and of a list page.
const (
	MaxTitleLength  = 200
	MaxDescription  = 20000
	MaxTags         = 20
	MaxTagLength    = 40
	DefaultPageSize = 100
	MaxPageSize     = 1000
)

// TagPattern is the grammar of a tag: lowercase letters, digits and `.`, `_`,
// `:`, `-`, starting with a letter or digit, at most MaxTagLength characters.
const TagPattern = `^[a-z0-9][a-z0-9._:-]{0,39}$`

var tagRE = regexp.MustCompile(TagPattern)

// ValidateTitle returns nil when title is 1 to MaxTitleLength characters once
// trimmed.
func ValidateTitle(title string) error {
	n := utf8.RuneCountInString(strings.TrimSpace(title))
	if n == 0 || n > MaxTitleLength {
		return fmt.Errorf("title must be 1-%d characters", MaxTitleLength)
	}
	return nil
}

// ValidateDescription returns nil when the Markdown description is at most
// MaxDescription characters.
func ValidateDescription(s string) error {
	if utf8.RuneCountInString(s) > MaxDescription {
		return fmt.Errorf("description must be at most %d characters", MaxDescription)
	}
	return nil
}

// NormalizeTags trims and lowercases every tag, checks each against
// TagPattern, drops repeats and returns them sorted. More than MaxTags
// distinct tags is an error.
func NormalizeTags(tags []string) ([]string, error) {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if !tagRE.MatchString(tag) {
			return nil, fmt.Errorf("tag %q must match [a-z0-9][a-z0-9._:-]{0,%d}", raw, MaxTagLength-1)
		}
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	if len(out) > MaxTags {
		return nil, fmt.Errorf("an initiative carries at most %d tags", MaxTags)
	}
	sort.Strings(out)
	return out, nil
}

// DateLayout is the form of a target date.
const DateLayout = "2006-01-02"

// ValidateDate returns nil when s is a calendar date like 2026-03-01.
func ValidateDate(s string) error {
	_, err := ParseDate(s)
	return err
}

// ParseDate reads a target date.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q must be YYYY-MM-DD", s)
	}
	return t, nil
}

// ValidateTargets checks the optional target dates of an initiative: each a
// calendar date, and the end not before the start.
func ValidateTargets(start, end string) error {
	for _, d := range []string{start, end} {
		if d == "" {
			continue
		}
		if err := ValidateDate(d); err != nil {
			return err
		}
	}
	if start != "" && end != "" && end < start {
		return errors.New("target_end must not be before target_start")
	}
	return nil
}

// Cursor is a parsed `after` value: a ULID, or an initiative ref, which is its
// slug.
type Cursor struct {
	ID   string
	Slug string
}

// IsZero reports whether the cursor names nothing, which starts at the first
// page.
func (c Cursor) IsZero() bool { return c.ID == "" && c.Slug == "" }

// ParseCursor reads an `after` value. Empty is the zero cursor. A ULID is 26
// uppercase Crockford base32 characters whose first is at most 7 (the
// timestamp is 48 bits), and a slug is lowercase, so they differ except for a
// 26-character slug of digits below 8 first, which reads as a ULID.
func ParseCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	if ValidateULID(s) == nil {
		return Cursor{ID: s}, nil
	}
	if ValidateSlug(s) == nil {
		return Cursor{Slug: s}, nil
	}
	return Cursor{}, fmt.Errorf("after %q is neither a ULID nor an initiative slug", s)
}

// ValidateULID returns nil when s has the text form NewULID writes.
func ValidateULID(s string) error {
	if len(s) != ULIDLength || s[0] > '7' {
		return fmt.Errorf("%q is not a ULID", s)
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(ulidAlphabet, s[i]) < 0 {
			return fmt.Errorf("%q is not a ULID", s)
		}
	}
	return nil
}

// PageSize resolves a `limit` parameter: zero (absent) is DefaultPageSize, and
// a value outside 1..MaxPageSize is an error.
func PageSize(limit int) (int, error) {
	switch {
	case limit == 0:
		return DefaultPageSize, nil
	case limit < 1 || limit > MaxPageSize:
		return 0, fmt.Errorf("limit must be between 1 and %d", MaxPageSize)
	}
	return limit, nil
}
