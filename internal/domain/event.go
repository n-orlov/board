package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxSummary is the longest summary of a check or a milestone, in characters.
const MaxSummary = 200

// ValidateSummary returns nil when summary is 1 to MaxSummary characters once
// trimmed.
func ValidateSummary(summary string) error {
	n := utf8.RuneCountInString(strings.TrimSpace(summary))
	if n == 0 || n > MaxSummary {
		return fmt.Errorf("summary must be 1-%d characters", MaxSummary)
	}
	return nil
}

// OneLine folds every run of whitespace, newlines included, into one space and
// trims the ends, so a text fits the one-line summary of an event.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Overdue is the derived state of a task: a watch task that is not closed and
// whose next_check_at is before now.
func Overdue(kind, status string, nextCheck *time.Time, now time.Time) bool {
	return kind == KindWatch && status != "closed" && nextCheck != nil && nextCheck.Before(now)
}

// FormatEventRef is the short ref of the event with sequence number n: E<n>.
func FormatEventRef(n int64) string { return "E" + strconv.FormatInt(n, 10) }

// ParseEventRef reads an E<n> short ref, with E in either case and n a positive
// decimal without a sign or leading zero.
func ParseEventRef(s string) (int64, bool) {
	if !eventRefRE.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	return n, err == nil
}

var eventRefRE = regexp.MustCompile(`^[Ee][1-9][0-9]*$`)

// Kinds of an EventPoint.
const (
	PointID   = "id"
	PointN    = "n"
	PointTime = "time"
)

// EventPoint is a place in the event log: after the event with this ULID or
// number, or (for a time) from that instant on.
type EventPoint struct {
	Kind string
	ID   string
	N    int64
	At   time.Time
}

// ParseEventPoint reads the value of `since`: an event ULID, an E<n> ref (either
// case) or an RFC 3339 time. Anything else is an error. With onlyEvents a time
// is an error too, which is how `after` reads its value.
func ParseEventPoint(s string, onlyEvents bool) (EventPoint, error) {
	if n, ok := ParseEventRef(s); ok {
		return EventPoint{Kind: PointN, N: n}, nil
	}
	if id, ok := KeysetULID(s); ok {
		return EventPoint{Kind: PointID, ID: id}, nil
	}
	if onlyEvents {
		return EventPoint{}, errors.New("value must be an event ULID or an E<n> ref")
	}
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return EventPoint{}, errors.New("value must be an event ULID, an E<n> ref or an RFC 3339 time")
	}
	return EventPoint{Kind: PointTime, At: at.UTC()}, nil
}
