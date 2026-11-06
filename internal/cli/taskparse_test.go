package cli

import "testing"

func TestParseDuration(t *testing.T) {
	good := map[string]int64{"1s": 1, "90s": 90, "5m": 300, "2h": 7200, "1d": 86400, "30d": 2592000, "007m": 420}
	for in, want := range good {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("parseDuration(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "90", "m", "-5m", "+5m", "1.5h", "abc", "5 m", "5M", "0s", "1w", "5mm", "99999999999999999999d", "9223372036854775807d"} {
		if got, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) = %d, want an error", in, got)
		}
	}
}

func TestFormatDurationIsTheShortestExactForm(t *testing.T) {
	for in, want := range map[int64]string{90: "90s", 60: "1m", 5400: "90m", 7200: "2h", 86400: "1d", 172800: "2d", 129600: "36h"} {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTime(t *testing.T) {
	good := map[string]string{
		"2026-03-04T09:30:00Z":      "2026-03-04T09:30:00Z",
		"2026-03-04T11:30:00+02:00": "2026-03-04T09:30:00Z",
		"2026-03-04":                "2026-03-04T00:00:00Z",
	}
	for in, want := range good {
		if got, err := parseTime(in); err != nil || got != want {
			t.Errorf("parseTime(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "tomorrow", "2026-02-30", "2026-3-4", "2026-03-04 09:30", "2026-03-04T09:30:00", "04/03/2026"} {
		if got, err := parseTime(in); err == nil {
			t.Errorf("parseTime(%q) = %q, want an error", in, got)
		}
	}
}

func TestParseLink(t *testing.T) {
	if r, err := parseLink("PR 12=https://example.com/pr?a=b"); err != nil || r.Label != "PR 12" || r.URL != "https://example.com/pr?a=b" {
		t.Errorf("got %+v, %v", r, err)
	}
	for _, in := range []string{"", "nolabel", "=https://x", "label=", " =x"} {
		if _, err := parseLink(in); err == nil {
			t.Errorf("parseLink(%q): want an error", in)
		}
	}
}
