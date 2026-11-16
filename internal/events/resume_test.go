package events

import (
	"fmt"
	"strings"
	"testing"

	"github.com/n-orlov/village-hall/internal/initiatives"
)

func TestResumeReadsTheEventsAfterAnIDUpToThePosition(t *testing.T) {
	f := newFixture(t)
	f.seed(t) // E1..E11
	all := list(t, f, Query{})
	id := all.Items[2].ID // E3
	for name, tc := range map[string]struct {
		last  string
		upTo  int64
		known bool
		from  int64
		want  []string
	}{
		"a ULID":                  {id, 11, true, 3, []string{"E4", "E5", "E6", "E7", "E8", "E9", "E10", "E11"}},
		"the ULID in lower case":  {strings.ToLower(id), 5, true, 3, []string{"E4", "E5"}},
		"an E ref":                {"E9", 11, true, 9, []string{"E10", "E11"}},
		"the newest event":        {all.Items[10].ID, 11, true, 11, nil},
		"ahead of the position":   {"E9", 5, true, 9, nil},
		"an event that is absent": {"E99", 11, false, 0, nil},
		"an unknown ULID":         {"01ARZ3NDEKTSV4RRFFQ69G5FAV", 11, false, 0, nil},
		"a time":                  {"2026-11-05T12:00:00Z", 11, false, 0, nil},
		"nothing":                 {"", 11, false, 0, nil},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := f.ev.Resume(ctx, tc.last, tc.upTo)
			if err != nil {
				t.Fatal(err)
			}
			if r.Known != tc.known || r.From != tc.from || r.TooMany {
				t.Errorf("Resume = known %v from %d too many %v, want known %v from %d", r.Known, r.From, r.TooMany, tc.known, tc.from)
			}
			if got := refs(Page{Items: r.Items}); fmt.Sprint(got) != fmt.Sprint(append([]string{}, tc.want...)) {
				t.Errorf("items = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResumeStopsAtMoreThanTheLimit(t *testing.T) {
	f := newFixture(t)
	for i := range ResumeLimit + 1 {
		_, err := f.in.Create(ctx, ann, "key:ann", initiatives.New{Slug: fmt.Sprintf("i%04d", i), Title: "T"})
		f.must(t, err)
	}
	// E1 is the id: events 2..1001 are 1000 missed events, one more is too many.
	r, err := f.ev.Resume(ctx, "E1", ResumeLimit+1)
	if err != nil || r.TooMany || len(r.Items) != ResumeLimit {
		t.Fatalf("1000 missed: too many %v, %d items, %v", r.TooMany, len(r.Items), err)
	}
	_, err = f.in.Create(ctx, ann, "key:ann", initiatives.New{Slug: "extra", Title: "T"})
	f.must(t, err)
	r, err = f.ev.Resume(ctx, "E1", ResumeLimit+2)
	if err != nil || !r.TooMany || len(r.Items) != 0 || !r.Known {
		t.Fatalf("1001 missed: %+v, %v", r, err)
	}
}
