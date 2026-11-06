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

// Cursor is a parsed `after` value: an initiative reference, a ULID or a slug
// in either letter case. Which row it names is settled against the stored ids
// and slugs (RefLookups), never by the text's shape alone.
type Cursor struct {
	Ref string
}

// IsZero reports whether the cursor names nothing, which starts at the first
// page.
func (c Cursor) IsZero() bool { return c.Ref == "" }

// ParseCursor reads an `after` value. Empty is the zero cursor. Anything else
// must be readable as a ULID or as a slug, in either letter case (SPEC 8.2): a
// 26-character slug whose text is also ULID-shaped is both, and
// the store decides which row it names.
func ParseCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	if len(RefLookups(s)) == 0 {
		return Cursor{}, fmt.Errorf("after %q is neither a ULID nor an initiative slug", s)
	}
	return Cursor{Ref: s}, nil
}

// RefLookup is one way to read an initiative reference: Value is compared with
// the id column when ByID, else with the slug column.
type RefLookup struct {
	ByID  bool
	Value string
}

// RefLookups lists, in the order to try them, every row a reference can name.
// References are case-insensitive on input (SPEC 8.2): ids are stored
// uppercase and slugs lowercase, so a lookup tries the text as given first,
// then its other case. A text that is a valid slug and also ULID-shaped (26
// characters of digits, or a ULID in lowercase) is therefore never taken for
// one kind by its shape: the first lookup that finds a row wins. An empty list
// means the text is neither grammar.
func RefLookups(ref string) []RefLookup {
	var out []RefLookup
	add := func(byID bool, v string, valid func(string) error) {
		l := RefLookup{ByID: byID, Value: v}
		if valid(v) != nil {
			return
		}
		for _, have := range out {
			if have == l {
				return
			}
		}
		out = append(out, l)
	}
	add(false, ref, ValidateSlug)
	add(true, ref, ValidateULID)
	add(true, strings.ToUpper(ref), ValidateULID)
	add(false, strings.ToLower(ref), ValidateSlug)
	return out
}

// KeysetULID returns the ULID a reference reads as when it is ULID-shaped in
// either case, for paging past an id that names no row.
func KeysetULID(ref string) (string, bool) {
	up := strings.ToUpper(ref)
	return up, ValidateULID(up) == nil
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
