package domain

import "testing"

func TestParseDaysReadsTheWindowsAndWholeDays(t *testing.T) {
	for in, want := range map[string]int{"7d": 7, "30d": 30, "90d": 90, "1": 1, "14": 14, "36500": 36500} {
		if got, err := ParseDays(in); err != nil || got != want {
			t.Errorf("ParseDays(%q) = %d, %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "-1", "36501", "1w", "7D", "x", "1.5"} {
		if _, err := ParseDays(in); err == nil {
			t.Errorf("ParseDays(%q) = nil error", in)
		}
	}
}
