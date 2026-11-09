package server

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/notes"
)

func TestNoteOperationsAnswer503WithoutAStore(t *testing.T) {
	ctx := context.Background()
	post, _ := noteOps{}.PostNote(ctx, PostNoteRequestObject{})
	list, _ := noteOps{}.ListNotes(ctx, ListNotesRequestObject{})
	get, _ := noteOps{}.GetNote(ctx, GetNoteRequestObject{Ref: "N1"})
	for name, got := range map[string]any{"post": post, "list": list, "get": get} {
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("%s without a store = %#v, want a 503 problem", name, got)
		}
	}
}

func TestPostingANoteWithoutAPrincipalIs401(t *testing.T) {
	got, _ := noteOps{notes: &notes.Store{}}.PostNote(context.Background(), PostNoteRequestObject{Body: &NotePost{Summary: "x"}})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("post without a principal = %#v, want 401", got)
	}
}

func TestNoteFailureMapsEachErrorToItsStatusAndSubject(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		detail string
	}{
		"invalid":    {notes.InvalidError{Msg: "bad", Next: "fix it"}, http.StatusBadRequest, "bad"},
		"task":       {errors.Join(errors.New("note not found: task aa-9"), notes.ErrNotFound), http.StatusNotFound, ""},
		"initiative": {errors.Join(errors.New("note not found: initiative zz"), notes.ErrNotFound), http.StatusNotFound, ""},
		"note":       {errors.Join(errors.New("note not found: note N9"), notes.ErrNotFound), http.StatusNotFound, ""},
	} {
		p, ok := noteFailure(tc.err)
		if !ok || p.status != tc.status || p.next == "" {
			t.Errorf("%s = %+v, %v; want status %d with a next", name, p, ok, tc.status)
		}
	}
	if _, ok := noteFailure(errors.New("disk")); ok {
		t.Error("a server error was mapped to a problem")
	}
	if _, err := noteResult[GetNoteResponseObject](errors.New("disk")); err == nil {
		t.Error("a server error was swallowed")
	}
	if r, err := noteResult[GetNoteResponseObject](notes.InvalidError{Msg: "x"}); err != nil || r == nil {
		t.Errorf("an invalid note = %v, %v; want a problem response", r, err)
	}
}

func TestNoteRequestCarriesEveryField(t *testing.T) {
	task, ini, body, resource, mentions := "aa-1", "aa", "b", "host:x", []string{"a@b"}
	got := noteRequest(&NotePost{Task: &task, Initiative: &ini, Summary: "s", Body: &body, Resource: &resource, Mentions: &mentions})
	want := notes.New{Task: "aa-1", Initiative: "aa", Summary: "s", Body: "b", Resource: "host:x", ResourceSet: true, Mentions: []string{"a@b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request = %+v, want %+v", got, want)
	}
	if bare := noteRequest(&NotePost{Summary: "s"}); bare.Mentions != nil || bare.Body != "" {
		t.Errorf("bare request = %+v, want the defaults left to the store", bare)
	} else if bare.ResourceSet {
		t.Errorf("an omitted resource is marked supplied: %+v", bare)
	}
	empty := ""
	if got := noteRequest(&NotePost{Summary: "s", Resource: &empty}); !got.ResourceSet || got.Resource != "" {
		t.Errorf("an empty supplied resource = %+v, want it marked supplied", got)
	}
}

func TestAPINoteLeavesTheBodyOutOfASummaryButKeepsHasBody(t *testing.T) {
	k := notes.Note{ID: "I", Ref: "N1", Actor: "a", Summary: "s", Body: "text", HasBody: true, Task: "aa-1", CreatedAt: time.Unix(0, 0)}
	if got := apiNote(k, false); got.Body != nil || !got.HasBody || got.Mentions == nil || got.Task == nil || *got.Task != "aa-1" {
		t.Errorf("summary note = %+v, want no body, has_body, empty mentions and the task", got)
	}
	if got := apiNote(k, true); got.Body == nil || *got.Body != "text" {
		t.Errorf("full note = %+v, want the body", got)
	}
	k.Body, k.HasBody = "", false
	if got := apiNote(k, true); got.Body != nil || got.HasBody {
		t.Errorf("note without a body = %+v, want neither", got)
	}
}

func TestFieldsFullReadsSummaryAndFullAndRefusesTheRest(t *testing.T) {
	summary, full, junk := "summary", "full", "everything"
	for name, tc := range map[string]struct {
		in   *string
		want bool
		bad  bool
	}{"absent": {nil, false, false}, "summary": {&summary, false, false}, "full": {&full, true, false}, "junk": {&junk, false, true}} {
		got, bad := fieldsFull(tc.in)
		if got != tc.want || (bad != nil) != tc.bad {
			t.Errorf("%s = %v, %v; want %v, refused %v", name, got, bad, tc.want, tc.bad)
		}
	}
}

func TestNoteQueryCarriesEveryFilter(t *testing.T) {
	s, task, ini, bc, m, a, f, after := "N1", "aa-1", "aa", "only", "x@y", "z@w", "full", "N9"
	res, limit := []string{"r:1", "r:2"}, EventLimit(5)
	got := noteQuery(ListNotesParams{Since: &s, Task: &task, Initiative: &ini, Resource: &res, Broadcast: &bc, Mention: &m,
		Author: &a, Fields: &f, Limit: &limit, After: &after}, true)
	want := notes.Query{Since: "N1", After: "N9", Task: "aa-1", Initiative: "aa", Resources: res, Broadcast: "only", Mention: "x@y",
		Author: "z@w", Full: true, Limit: 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("query = %+v, want %+v", got, want)
	}
}
