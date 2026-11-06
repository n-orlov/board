package domain

import (
	"fmt"
	"strconv"
)

// ParseDays reads a closed_within value: 7d, 30d, 90d or a whole number of days
// from 1 to 36500.
func ParseDays(s string) (int, error) {
	switch s {
	case "7d":
		return 7, nil
	case "30d":
		return 30, nil
	case "90d":
		return 90, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 36500 {
		return 0, fmt.Errorf("closed_within %q is not 7d, 30d, 90d or a number of days from 1 to 36500", s)
	}
	return n, nil
}
