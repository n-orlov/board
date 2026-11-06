package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Task kinds.
const (
	KindWork  = "work"
	KindWatch = "watch"
)

// Limits of a task's fields.
const (
	MinCadenceSeconds = 60
	MaxCadenceSeconds = 31536000
	MaxExternalRefs   = 20
	MaxRefLabel       = 80
)

// ExternalRef is a link from a task to a ticket, pull request or build.
type ExternalRef struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// TaskRef is a parsed task reference: either an ID (a ULID, uppercase) or an
// initiative Slug (lowercase) with the task's Seq in it.
type TaskRef struct {
	ID   string
	Slug string
	Seq  int
}

// ParseTaskRef reads `<slug>-<seq>` or a ULID, in either letter case (SPEC
// 8.2). The seq is a positive integer. A ULID holds no hyphen, so the two forms
// never overlap.
func ParseTaskRef(ref string) (TaskRef, error) {
	if up := strings.ToUpper(ref); ValidateULID(up) == nil {
		return TaskRef{ID: up}, nil
	}
	cut := strings.LastIndexByte(ref, '-')
	if cut < 0 {
		return TaskRef{}, fmt.Errorf("task ref %q must be <initiative-slug>-<number> or a ULID", ref)
	}
	slug, num := strings.ToLower(ref[:cut]), ref[cut+1:]
	seq, err := parseSeq(num)
	if err != nil || ValidateSlug(slug) != nil {
		return TaskRef{}, fmt.Errorf("task ref %q must be <initiative-slug>-<number> or a ULID", ref)
	}
	return TaskRef{Slug: slug, Seq: seq}, nil
}

// parseSeq reads a task number: ASCII digits only, at least 1.
func parseSeq(s string) (int, error) {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errors.New("not a number")
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, errors.New("not a positive number")
	}
	return n, nil
}

// FormatTaskRef is the short ref of the task numbered seq in the initiative
// slug.
func FormatTaskRef(slug string, seq int) string { return slug + "-" + strconv.Itoa(seq) }

// ValidateKind returns nil for `work` and `watch`.
func ValidateKind(kind string) error {
	if kind != KindWork && kind != KindWatch {
		return errors.New("kind must be work or watch")
	}
	return nil
}

// ValidateCadence returns nil when a watch task's cadence is
// MinCadenceSeconds to MaxCadenceSeconds.
func ValidateCadence(seconds int64) error {
	if seconds < MinCadenceSeconds || seconds > MaxCadenceSeconds {
		return fmt.Errorf("cadence_seconds must be %d-%d", MinCadenceSeconds, MaxCadenceSeconds)
	}
	return nil
}

// ValidateWatchFields checks the watch-only fields against the kind: a work
// task carries none, and a watch task carries a valid cadence.
func ValidateWatchFields(kind string, cadence *int64, nextCheck *time.Time) error {
	if kind == KindWork {
		if cadence != nil || nextCheck != nil {
			return errors.New("cadence_seconds and next_check_at are for watch tasks only")
		}
		return nil
	}
	if cadence == nil {
		return errors.New("a watch task needs cadence_seconds")
	}
	return ValidateCadence(*cadence)
}

// ValidatePlanned returns nil when the planned end is not before the planned
// start (either may be absent).
func ValidatePlanned(start, end *time.Time) error {
	if start != nil && end != nil && end.Before(*start) {
		return errors.New("planned_end must not be before planned_start")
	}
	return nil
}

// ValidateExternalRefs checks at most MaxExternalRefs links, each with a label
// of 1 to MaxRefLabel characters and an http or https URL with a host.
func ValidateExternalRefs(refs []ExternalRef) error {
	if len(refs) > MaxExternalRefs {
		return fmt.Errorf("a task carries at most %d external_refs", MaxExternalRefs)
	}
	for i, r := range refs {
		if n := utf8.RuneCountInString(r.Label); n < 1 || n > MaxRefLabel {
			return fmt.Errorf("external_refs[%d].label must be 1-%d characters", i, MaxRefLabel)
		}
		if !isWebURL(r.URL) {
			return fmt.Errorf("external_refs[%d].url must be an http or https URL", i)
		}
	}
	return nil
}

// isWebURL reports whether s is an http or https URL with a host.
func isWebURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
