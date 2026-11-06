package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of a lease (SPEC 4.4).
const (
	MinLeaseTTLSeconds     = 60
	MaxLeaseTTLSeconds     = 43200
	DefaultLeaseTTLSeconds = 7200
	MaxIntent              = 200
	MaxResources           = 20
	MaxResourceLen         = 100
)

// Lease states.
const (
	LeaseLive    = "live"
	LeaseEnded   = "ended"
	LeaseExpired = "expired"
)

// ClampLeaseTTL returns the ttl a lease takes: the requested seconds clamped
// to [MinLeaseTTLSeconds, MaxLeaseTTLSeconds], or DefaultLeaseTTLSeconds when
// none was requested.
func ClampLeaseTTL(requested *int64) int64 {
	switch {
	case requested == nil:
		return DefaultLeaseTTLSeconds
	case *requested < MinLeaseTTLSeconds:
		return MinLeaseTTLSeconds
	case *requested > MaxLeaseTTLSeconds:
		return MaxLeaseTTLSeconds
	}
	return *requested
}

// ValidateIntent checks a lease's intent: 1 to 200 characters.
func ValidateIntent(intent string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(intent)); n < 1 || n > MaxIntent {
		return fmt.Errorf("intent must be 1 to %d characters, got %d", MaxIntent, n)
	}
	return nil
}

// NormalizeResources removes duplicate resources (the first of each stays, in
// order) and checks that each is 1 to 100 characters and that at most 20
// remain. It returns a non-nil slice.
func NormalizeResources(resources []string) ([]string, error) {
	out := make([]string, 0, len(resources))
	seen := map[string]bool{}
	for _, r := range resources {
		if n := utf8.RuneCountInString(r); n < 1 || n > MaxResourceLen {
			return nil, fmt.Errorf("a resource must be 1 to %d characters, got %d", MaxResourceLen, n)
		}
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	if len(out) > MaxResources {
		return nil, fmt.Errorf("a lease holds at most %d resources, got %d", MaxResources, len(out))
	}
	return out, nil
}

// FormatLeaseRef is the short ref L<n> of the lease numbered n.
func FormatLeaseRef(n int64) string { return "L" + strconv.FormatInt(n, 10) }

// ParseLeaseRef reads L<n> (L in either case, n a positive integer without a
// sign or leading zero).
func ParseLeaseRef(s string) (int64, bool) {
	if len(s) < 2 || (s[0] != 'L' && s[0] != 'l') {
		return 0, false
	}
	n, err := parseSeq(s[1:])
	return int64(n), err == nil
}

// Holding is a live lease as the conflict computation sees it.
type Holding struct {
	Lease     string
	Holder    string
	Task      string
	Intent    string
	ExpiresAt time.Time
	Resources []string
}

// Conflicts returns the holdings in live that share a resource with the lease
// named lease holding resources, each carrying only the shared resources (in
// the order of resources). A lease never conflicts with itself, so the holding
// of the same lease id is skipped, and holdings of the same actor on other
// tasks are not. The order of live is kept.
func Conflicts(lease string, resources []string, live []Holding) []Holding {
	out := []Holding{}
	for _, h := range live {
		if h.Lease == lease {
			continue
		}
		theirs := map[string]bool{}
		for _, r := range h.Resources {
			theirs[r] = true
		}
		var shared []string
		for _, r := range resources {
			if theirs[r] {
				shared = append(shared, r)
			}
		}
		if len(shared) > 0 {
			h.Resources = shared
			out = append(out, h)
		}
	}
	return out
}
