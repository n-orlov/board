package domain

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxHandleLen is the longest handle, in characters (SPEC 9).
const MaxHandleLen = 100

// ValidateHandle reports why h is not an actor handle (SPEC 9): it is 1 to
// MaxHandleLen characters with no whitespace and no control character. The
// caller trims it first.
func ValidateHandle(h string) error {
	switch {
	case h == "":
		return fmt.Errorf("a handle is not empty")
	case utf8.RuneCountInString(h) > MaxHandleLen:
		return fmt.Errorf("a handle is at most %d characters", MaxHandleLen)
	case strings.IndexFunc(h, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return fmt.Errorf("a handle has no whitespace or control character, got %q", h)
	}
	return nil
}
