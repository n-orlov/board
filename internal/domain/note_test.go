package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNoteRefs(t *testing.T) {
	if FormatNoteRef(40) != "N40" {
		t.Error("FormatNoteRef")
	}
	for in, want := range map[string]int64{"N1": 1, "n40": 40, "N007x": 0} {
		n, ok := ParseNoteRef(in)
		if want == 0 {
			if ok {
				t.Errorf("ParseNoteRef(%q) ok", in)
			}
		} else if !ok || n != want {
			t.Errorf("ParseNoteRef(%q) = %d, %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "N", "N0", "N-1", "N01", "A3", "3", "N+1", "N99999999999999999999"} {
		if _, ok := ParseNoteRef(in); ok {
			t.Errorf("ParseNoteRef(%q) ok", in)
		}
	}
}

func TestValidateNoteSummaryAndBody(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{"", false}, {"  ", false}, {"a", true}, {strings.Repeat("é", 200), true}, {strings.Repeat("é", 201), false},
		{"two\nlines", false}, {"cr\rhere", false}, {" padded ", true},
	} {
		if err := ValidateNoteSummary(tc.in); (err == nil) != tc.ok {
			t.Errorf("ValidateNoteSummary(%q) = %v, want ok=%v", tc.in, err, tc.ok)
		}
	}
	if ValidateNoteBody(strings.Repeat("é", 20000)) != nil || ValidateNoteBody(strings.Repeat("é", 20001)) == nil || ValidateNoteBody("") != nil {
		t.Error("ValidateNoteBody bounds")
	}
}

func TestNormalizeMentions(t *testing.T) {
	got, err := NormalizeMentions([]string{" a@h ", "", "b@h", "a@h"})
	if err != nil || strings.Join(got, ",") != "a@h,b@h" {
		t.Errorf("NormalizeMentions = %v, %v", got, err)
	}
	if got, err := NormalizeMentions(nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("NormalizeMentions(nil) = %#v, %v", got, err)
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = "h" + strings.Repeat("x", i)
	}
	if _, err := NormalizeMentions(many[:20]); err != nil {
		t.Errorf("20 mentions: %v", err)
	}
	for name, in := range map[string][]string{"too many": many, "long": {strings.Repeat("a", 101)}, "space": {"a b"}, "tab": {"a\tb"}} {
		if _, err := NormalizeMentions(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NormalizeMentions([]string{strings.Repeat("a", 100)}); err != nil {
		t.Errorf("100 chars: %v", err)
	}
}

func TestParseNotePoint(t *testing.T) {
	id := "01J00000000000000000000000"
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   string
		want NotePoint
	}{
		{"n7", NotePoint{Kind: PointN, N: 7}},
		{id, NotePoint{Kind: PointID, ID: id}},
		{strings.ToLower(id), NotePoint{Kind: PointID, ID: id}},
		{"2026-11-09T14:00:00+02:00", NotePoint{Kind: PointTime, At: at}},
	} {
		got, err := ParseNotePoint(tc.in)
		if err != nil || got.Kind != tc.want.Kind || got.ID != tc.want.ID || got.N != tc.want.N || !got.At.Equal(tc.want.At) {
			t.Errorf("ParseNotePoint(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"", "E7", "yesterday", "2026-11-09"} {
		if _, err := ParseNotePoint(in); err == nil {
			t.Errorf("ParseNotePoint(%q) accepted", in)
		}
	}
}
