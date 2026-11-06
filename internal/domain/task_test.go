package domain

import (
	"strings"
	"testing"
	"time"
)

const sampleULID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestParseTaskRef(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want TaskRef
		ok   bool
	}{
		{"slug and seq", "phone-onboard-3", TaskRef{Slug: "phone-onboard", Seq: 3}, true},
		{"upper case slug", "Phone-Onboard-3", TaskRef{Slug: "phone-onboard", Seq: 3}, true},
		{"a hyphenless two letter slug", "ab-12", TaskRef{Slug: "ab", Seq: 12}, true},
		{"a ULID", sampleULID, TaskRef{ID: sampleULID}, true},
		{"a lower case ULID", strings.ToLower(sampleULID), TaskRef{ID: sampleULID}, true},
		{"no seq", "phone-onboard", TaskRef{}, false},
		{"no hyphen", "phone", TaskRef{}, false},
		{"trailing hyphen", "phone-", TaskRef{}, false},
		{"seq 0", "phone-0", TaskRef{}, false},
		{"seq 00", "phone-00", TaskRef{}, false},
		{"signed seq", "phone-+1", TaskRef{}, false},
		{"seq too large", "phone-99999999999999999999", TaskRef{}, false},
		{"invalid slug underscore", "ph_one-1", TaskRef{}, false},
		{"slug too short", "a-1", TaskRef{}, false},
		{"slug too long", strings.Repeat("a", 41) + "-1", TaskRef{}, false},
		{"empty", "", TaskRef{}, false},
		{"a ULID with a bad first digit", "81ARZ3NDEKTSV4RRFFQ69G5FAV", TaskRef{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTaskRef(tc.in)
			if (err == nil) != tc.ok || got != tc.want {
				t.Fatalf("ParseTaskRef(%q) = %+v, %v; want %+v ok=%v", tc.in, got, err, tc.want, tc.ok)
			}
		})
	}
}

func TestFormatTaskRef(t *testing.T) {
	if got := FormatTaskRef("a-b", 12); got != "a-b-12" {
		t.Fatalf("FormatTaskRef = %q", got)
	}
}

func TestTaskTitleAndDescriptionLimits(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		ok   bool
	}{
		{"empty title", ValidateTitle(""), false},
		{"blank title", ValidateTitle("  "), false},
		{"title of 200", ValidateTitle(strings.Repeat("é", 200)), true},
		{"title of 201", ValidateTitle(strings.Repeat("a", 201)), false},
		{"description of 20000", ValidateDescription(strings.Repeat("a", 20000)), true},
		{"description of 20001", ValidateDescription(strings.Repeat("a", 20001)), false},
	} {
		if (tc.err == nil) != tc.ok {
			t.Errorf("%s: %v, want ok=%v", tc.name, tc.err, tc.ok)
		}
	}
}

func TestValidateKind(t *testing.T) {
	for kind, ok := range map[string]bool{"work": true, "watch": true, "": false, "Work": false, "other": false} {
		if (ValidateKind(kind) == nil) != ok {
			t.Errorf("ValidateKind(%q) ok=%v, want %v", kind, !ok, ok)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestValidateCadenceBounds(t *testing.T) {
	for sec, ok := range map[int64]bool{59: false, 60: true, 61: true, 31536000: true, 31536001: false, 0: false, -60: false} {
		if (ValidateCadence(sec) == nil) != ok {
			t.Errorf("ValidateCadence(%d) ok=%v, want %v", sec, !ok, ok)
		}
	}
}

func TestValidateWatchFields(t *testing.T) {
	at := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		kind    string
		cadence *int64
		next    *time.Time
		ok      bool
	}{
		{"work with none", KindWork, nil, nil, true},
		{"work with a cadence", KindWork, ptr(int64(60)), nil, false},
		{"work with a next check", KindWork, nil, &at, false},
		{"watch with a cadence", KindWatch, ptr(int64(60)), nil, true},
		{"watch with both", KindWatch, ptr(int64(3600)), &at, true},
		{"watch without a cadence", KindWatch, nil, &at, false},
		{"watch cadence 59", KindWatch, ptr(int64(59)), nil, false},
		{"watch cadence 31536001", KindWatch, ptr(int64(31536001)), nil, false},
	}
	for _, tc := range tests {
		if err := ValidateWatchFields(tc.kind, tc.cadence, tc.next); (err == nil) != tc.ok {
			t.Errorf("%s: %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestValidatePlanned(t *testing.T) {
	a := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	b := a.Add(time.Millisecond)
	tests := []struct {
		name       string
		start, end *time.Time
		ok         bool
	}{
		{"neither", nil, nil, true},
		{"only a start", &a, nil, true},
		{"only an end", nil, &a, true},
		{"equal", &a, &a, true},
		{"end after start", &a, &b, true},
		{"end before start", &b, &a, false},
	}
	for _, tc := range tests {
		if err := ValidatePlanned(tc.start, tc.end); (err == nil) != tc.ok {
			t.Errorf("%s: %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func refs(n int) []ExternalRef {
	out := make([]ExternalRef, n)
	for i := range out {
		out[i] = ExternalRef{Label: "pr", URL: "https://example.com/pr/1"}
	}
	return out
}

func TestValidateExternalRefs(t *testing.T) {
	tests := []struct {
		name string
		refs []ExternalRef
		ok   bool
	}{
		{"none", nil, true},
		{"20 refs", refs(20), true},
		{"21 refs", refs(21), false},
		{"label of 1", []ExternalRef{{Label: "x", URL: "http://example.com"}}, true},
		{"label of 80", []ExternalRef{{Label: strings.Repeat("é", 80), URL: "https://example.com"}}, true},
		{"label of 0", []ExternalRef{{Label: "", URL: "https://example.com"}}, false},
		{"label of 81", []ExternalRef{{Label: strings.Repeat("a", 81), URL: "https://example.com"}}, false},
		{"upper case scheme", []ExternalRef{{Label: "x", URL: "HTTPS://example.com"}}, true},
		{"ftp url", []ExternalRef{{Label: "x", URL: "ftp://example.com/f"}}, false},
		{"javascript url", []ExternalRef{{Label: "x", URL: "javascript:alert(1)"}}, false},
		{"no scheme", []ExternalRef{{Label: "x", URL: "example.com/pr"}}, false},
		{"no host", []ExternalRef{{Label: "x", URL: "https://"}}, false},
		{"unparsable", []ExternalRef{{Label: "x", URL: "http://[::1"}}, false},
		{"second ref bad", []ExternalRef{{Label: "x", URL: "http://a.b"}, {Label: "", URL: "http://a.b"}}, false},
	}
	for _, tc := range tests {
		if err := ValidateExternalRefs(tc.refs); (err == nil) != tc.ok {
			t.Errorf("%s: %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestValidateResolution(t *testing.T) {
	for _, r := range []string{"completed", "dropped", "superseded", "duplicate"} {
		if err := ValidateResolution(r); err != nil {
			t.Errorf("%q: %v", r, err)
		}
	}
	for _, r := range []string{"", "Completed", "done", " dropped", "wontfix"} {
		if ValidateResolution(r) == nil {
			t.Errorf("%q accepted", r)
		}
	}
}
