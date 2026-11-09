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

// Limits of a note (SPEC 4.7).
const (
	MaxNoteSummary = 200
	MaxNoteBody    = 20000
	MaxMentions    = 20
	MaxMentionLen  = 100
)

// Notes filter values of `broadcast`.
const (
	BroadcastOnly    = "only"
	BroadcastExclude = "exclude"
)

// FormatNoteRef is the short ref N<n> of the note numbered n.
func FormatNoteRef(n int64) string { return "N" + strconv.FormatInt(n, 10) }

var noteRefRE = regexp.MustCompile(`^[Nn][1-9][0-9]*$`)

// ParseNoteRef reads N<n> (N in either case, n a positive integer without a sign).
func ParseNoteRef(s string) (int64, bool) {
	if !noteRefRE.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	return n, err == nil
}

// ValidateNoteSummary checks a note's summary: 1 to 200 characters once
// trimmed, on one line.
func ValidateNoteSummary(s string) error {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n < 1 || n > MaxNoteSummary {
		return fmt.Errorf("summary must be 1 to %d characters, got %d", MaxNoteSummary, n)
	}
	if strings.ContainsAny(s, "\r\n") {
		return errors.New("summary must be one line")
	}
	return nil
}

// ValidateNoteBody checks a note's Markdown body: at most 20000 characters.
func ValidateNoteBody(s string) error {
	if n := utf8.RuneCountInString(s); n > MaxNoteBody {
		return fmt.Errorf("body must be at most %d characters, got %d", MaxNoteBody, n)
	}
	return nil
}

// NormalizeMentions trims every handle, drops empty ones and repeats (the
// first of each stays, in order) and checks that each is at most 100
// characters with no whitespace and that at most 20 remain. It returns a
// non-nil slice.
func NormalizeMentions(handles []string) ([]string, error) {
	out := make([]string, 0, len(handles))
	seen := map[string]bool{}
	for _, h := range handles {
		h = strings.TrimSpace(h)
		switch {
		case h == "":
			continue
		case utf8.RuneCountInString(h) > MaxMentionLen:
			return nil, fmt.Errorf("a mention must be at most %d characters", MaxMentionLen)
		case strings.IndexFunc(h, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }) >= 0:
			return nil, fmt.Errorf("a mention is a handle without spaces, got %q", h)
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if len(out) > MaxMentions {
		return nil, fmt.Errorf("a note mentions at most %d handles, got %d", MaxMentions, len(out))
	}
	return out, nil
}

// NotePoint is a place in the notes: after the note with this ULID or number,
// or (for a time) from that instant on.
type NotePoint struct {
	Kind string
	ID   string
	N    int64
	At   time.Time
}

// ParseNotePoint reads the value of `since`: a note ULID, an N<n> ref (either
// case) or an RFC 3339 time. Anything else is an error.
func ParseNotePoint(s string) (NotePoint, error) {
	if n, ok := ParseNoteRef(s); ok {
		return NotePoint{Kind: PointN, N: n}, nil
	}
	if id, ok := KeysetULID(s); ok {
		return NotePoint{Kind: PointID, ID: id}, nil
	}
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return NotePoint{}, errors.New("value must be a note ULID, an N<n> ref or an RFC 3339 time")
	}
	return NotePoint{Kind: PointTime, At: at.UTC()}, nil
}
