package domain

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		ok    bool
	}{
		{"one character is the minimum", "a", true},
		{"200 characters is the maximum", strings.Repeat("a", 200), true},
		{"201 characters is too long", strings.Repeat("a", 201), false},
		{"empty is rejected", "", false},
		{"blank is rejected", "  \t", false},
		{"multibyte characters count once", strings.Repeat("é", 200), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTitle(tc.title); (err == nil) != tc.ok {
				t.Fatalf("ValidateTitle = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestValidateDescription(t *testing.T) {
	if err := ValidateDescription(strings.Repeat("x", MaxDescription)); err != nil {
		t.Errorf("the maximum is refused: %v", err)
	}
	if err := ValidateDescription(strings.Repeat("x", MaxDescription+1)); err == nil {
		t.Error("one past the maximum is accepted")
	}
	if err := ValidateDescription(""); err != nil {
		t.Errorf("empty is refused: %v", err)
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{" Work ", "homelab", "work", "repo:board"})
	if err != nil || !reflect.DeepEqual(got, []string{"homelab", "repo:board", "work"}) {
		t.Fatalf("NormalizeTags = %v, %v", got, err)
	}
	if got, err := NormalizeTags(nil); err != nil || len(got) != 0 {
		t.Errorf("no tags = %v, %v", got, err)
	}
	for _, bad := range []string{"", " ", "-lead", "has space", "slash/ed", strings.Repeat("a", 41)} {
		if _, err := NormalizeTags([]string{bad}); err == nil {
			t.Errorf("tag %q is accepted", bad)
		}
	}
	if _, err := NormalizeTags([]string{strings.Repeat("a", 40)}); err != nil {
		t.Errorf("a 40-character tag is refused: %v", err)
	}
}

func TestNormalizeTagsLimitsDistinctTags(t *testing.T) {
	var tags []string
	for i := 0; i < MaxTags; i++ {
		tags = append(tags, "t"+string(rune('a'+i)))
	}
	if _, err := NormalizeTags(append(tags, tags...)); err != nil {
		t.Errorf("%d distinct tags repeated are refused: %v", MaxTags, err)
	}
	if _, err := NormalizeTags(append(tags, "extra")); err == nil {
		t.Errorf("%d distinct tags are accepted", MaxTags+1)
	}
}

func TestValidateTargets(t *testing.T) {
	tests := []struct {
		name       string
		start, end string
		ok         bool
	}{
		{"neither", "", "", true},
		{"start only", "2026-03-01", "", true},
		{"end only", "", "2026-03-01", true},
		{"same day", "2026-03-01", "2026-03-01", true},
		{"end after start", "2026-03-01", "2026-04-01", true},
		{"end before start", "2026-04-01", "2026-03-01", false},
		{"not a date", "soon", "", false},
		{"month 13", "", "2026-13-01", false},
		{"with a time", "2026-03-01T00:00:00Z", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTargets(tc.start, tc.end); (err == nil) != tc.ok {
				t.Fatalf("ValidateTargets(%q, %q) = %v, want ok=%v", tc.start, tc.end, err, tc.ok)
			}
		})
	}
}

func TestParseCursor(t *testing.T) {
	id, err := NewULID(time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want Cursor
		ok   bool
	}{
		{"empty is the first page", "", Cursor{}, true},
		{"a ULID", id, Cursor{ID: id}, true},
		{"a slug", "phone-onboard", Cursor{Slug: "phone-onboard"}, true},
		{"a two-character slug", "ab", Cursor{Slug: "ab"}, true},
		{"a lowercase ULID is no ULID and no slug", strings.ToLower(id)[:25] + "!", Cursor{}, false},
		{"a ULID with a bad first character", "8" + id[1:], Cursor{}, false},
		{"a ULID with the letter U", id[:25] + "U", Cursor{}, false},
		{"a 27-character ULID", id + "0", Cursor{}, false},
		{"an uppercase slug", "Phone", Cursor{}, false},
		{"one character", "a", Cursor{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCursor(tc.in)
			if (err == nil) != tc.ok || got != tc.want {
				t.Fatalf("ParseCursor(%q) = %+v, %v; want %+v ok=%v", tc.in, got, err, tc.want, tc.ok)
			}
		})
	}
	if !(Cursor{}).IsZero() || (Cursor{Slug: "ab"}).IsZero() || (Cursor{ID: id}).IsZero() {
		t.Error("IsZero is wrong")
	}
}

func TestValidateULIDAcceptsWhatNewULIDMakes(t *testing.T) {
	for _, at := range []time.Time{time.Unix(0, 0), time.Now(), time.UnixMilli(1<<47 - 1)} {
		id, err := NewULID(at)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateULID(id); err != nil {
			t.Errorf("ValidateULID(%q) = %v", id, err)
		}
	}
}

func TestPageSize(t *testing.T) {
	tests := []struct {
		limit int
		want  int
		ok    bool
	}{
		{0, DefaultPageSize, true},
		{1, 1, true},
		{MaxPageSize, MaxPageSize, true},
		{MaxPageSize + 1, 0, false},
		{-1, 0, false},
	}
	for _, tc := range tests {
		got, err := PageSize(tc.limit)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("PageSize(%d) = %d, %v; want %d ok=%v", tc.limit, got, err, tc.want, tc.ok)
		}
	}
}
