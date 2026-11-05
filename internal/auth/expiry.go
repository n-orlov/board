package auth

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// MaxExpiryDays is the longest expiry a key may be given.
const MaxExpiryDays = 36500

var expiryPattern = regexp.MustCompile(`^([1-9][0-9]*)([dh])$`)

// ParseExpiry reads an expiry: a positive count of days (`90d`) or hours
// (`12h`), at most MaxExpiryDays days. The error reads after the name of the
// field it came from.
func ParseExpiry(s string) (time.Duration, error) {
	m := expiryPattern.FindStringSubmatch(s)
	if m == nil || len(m[1]) > 5 {
		return 0, fmt.Errorf("%q: want a number of days or hours, like 90d or 12h", s)
	}
	n, _ := strconv.Atoi(m[1])
	unit := 24 * time.Hour
	if m[2] == "h" {
		unit = time.Hour
	}
	if d := time.Duration(n) * unit; d <= MaxExpiryDays*24*time.Hour {
		return d, nil
	}
	return 0, fmt.Errorf("%q is longer than %d days", s, MaxExpiryDays)
}
