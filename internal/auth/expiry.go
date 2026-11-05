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
// (`12h`), at most MaxExpiryDays days in either unit. The error reads after
// the name of the field it came from.
func ParseExpiry(s string) (time.Duration, error) {
	m := expiryPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%q: want a number of days or hours, like 90d or 12h", s)
	}
	unit, limit := 24*time.Hour, int64(MaxExpiryDays)
	if m[2] == "h" {
		unit, limit = time.Hour, MaxExpiryDays*24
	}
	// A count too large for int64 is past the limit too.
	if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n <= limit {
		return time.Duration(n) * unit, nil
	}
	return 0, fmt.Errorf("%q is longer than %d days", s, MaxExpiryDays)
}
