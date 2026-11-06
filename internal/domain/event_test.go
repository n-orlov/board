package domain

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSummary(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"", false}, {"   ", false}, {"a", true}, {strings.Repeat("a", 200), true}, {strings.Repeat("a", 201), false},
		{strings.Repeat("é", 200), true}, {strings.Repeat("é", 201), false}, {" " + strings.Repeat("a", 200) + " ", true},
	} {
		if err := ValidateSummary(tc.in); (err == nil) != tc.ok {
			t.Errorf("ValidateSummary(%d bytes) = %v, want ok=%v", len(tc.in), err, tc.ok)
		}
	}
}

func TestOneLine(t *testing.T) {
	if got := OneLine("  a\n\tb   c\r\n"); got != "a b c" {
		t.Errorf("OneLine = %q", got)
	}
}

func TestOverdue(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	for _, tc := range []struct {
		name         string
		kind, status string
		next         *time.Time
		want         bool
	}{
		{"open watch in the past", KindWatch, "open", &past, true},
		{"done watch in the past", KindWatch, "done", &past, true},
		{"closed watch", KindWatch, "closed", &past, false},
		{"future", KindWatch, "open", &future, false},
		{"exactly now", KindWatch, "open", &now, false},
		{"no time", KindWatch, "open", nil, false},
		{"work", KindWork, "open", &past, false},
	} {
		if got := Overdue(tc.kind, tc.status, tc.next, now); got != tc.want {
			t.Errorf("%s: Overdue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEventRef(t *testing.T) {
	if got := FormatEventRef(42); got != "E42" {
		t.Errorf("FormatEventRef = %q", got)
	}
	for in, want := range map[string]int64{"E1": 1, "e1": 1, "E42": 42, "e9223372036854775807": 9223372036854775807} {
		if n, ok := ParseEventRef(in); !ok || n != want {
			t.Errorf("ParseEventRef(%q) = %d, %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "E", "E0", "E01", "E-1", "E+1", "E1x", "1", "X1", "e", "E9223372036854775808", sampleULID} {
		if n, ok := ParseEventRef(in); ok {
			t.Errorf("ParseEventRef(%q) = %d, want refused", in, n)
		}
	}
}

func TestParseEventPoint(t *testing.T) {
	at := time.Date(2026, 11, 6, 12, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		only bool
		want EventPoint
		ok   bool
	}{
		{sampleULID, false, EventPoint{Kind: PointID, ID: sampleULID}, true},
		{strings.ToLower(sampleULID), false, EventPoint{Kind: PointID, ID: sampleULID}, true},
		{"E7", false, EventPoint{Kind: PointN, N: 7}, true},
		{"e7", true, EventPoint{Kind: PointN, N: 7}, true},
		{"2026-11-06T12:30:00Z", false, EventPoint{Kind: PointTime, At: at}, true},
		{"2026-11-06T14:30:00+02:00", false, EventPoint{Kind: PointTime, At: at}, true},
		{"2026-11-06T12:30:00Z", true, EventPoint{}, false},
		{sampleULID, true, EventPoint{Kind: PointID, ID: sampleULID}, true},
		{"2026-11-06", false, EventPoint{}, false},
		{"yesterday", false, EventPoint{}, false},
		{"", false, EventPoint{}, false},
		{"E0", false, EventPoint{}, false},
	} {
		got, err := ParseEventPoint(tc.in, tc.only)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseEventPoint(%q, %v) = %+v, %v; want %+v ok=%v", tc.in, tc.only, got, err, tc.want, tc.ok)
		}
	}
}
