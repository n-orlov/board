package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/n-orlov/village-hall/internal/cursors"
)

func TestCursorOperationsAnswer503WithoutAStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	put, _ := cursorOps{}.PutCursor(ctx, PutCursorRequestObject{Stream: "notes", Body: &CursorSet{Position: "N1"}})
	if p, ok := put.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("put without a store = %#v, want a 503 problem", put)
	}
	_, problem, err := cursorOps{}.readFrom(ctx, cursors.StreamNotes, sinceCursor)
	if err != nil || problem == nil || problem.status != http.StatusServiceUnavailable {
		t.Errorf("since=cursor without a store = %#v, %v, want a 503 problem", problem, err)
	}
	list, _ := noteOps{}.ListNotes(ctx, ListNotesRequestObject{})
	if p, ok := list.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("list without a store = %#v", list)
	}
}

func TestCursorOperationsWithoutAPrincipalAre401(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o := cursorOps{cursors: &cursors.Store{}}
	put, _ := o.PutCursor(ctx, PutCursorRequestObject{Stream: "notes", Body: &CursorSet{Position: "N1"}})
	if p, ok := put.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("put without a principal = %#v, want 401", put)
	}
	_, problem, err := o.readFrom(ctx, cursors.StreamEvents, sinceCursor)
	if err != nil || problem == nil || problem.status != http.StatusUnauthorized {
		t.Errorf("since=cursor without a principal = %#v, %v, want 401", problem, err)
	}
}

func TestReadFromPassesEveryOtherSinceThrough(t *testing.T) {
	t.Parallel()
	for _, since := range []string{"", "N5", "2026-11-09T12:00:00Z", "Cursor"} {
		got, problem, err := cursorOps{}.readFrom(context.Background(), cursors.StreamNotes, since)
		if got != since || problem != nil || err != nil {
			t.Errorf("readFrom(%q) = %q, %v, %v", since, got, problem, err)
		}
	}
}

func TestCursorFailureMapsEachError(t *testing.T) {
	t.Parallel()
	bad, ok := cursorFailure(cursors.InvalidError{Msg: "position \"N9\" names no note", Next: "send one"})
	if !ok || bad.status != http.StatusBadRequest {
		t.Errorf("invalid = %#v, %v, want 400", bad, ok)
	}
	missing, ok := cursorFailure(fmt.Errorf("%w: stream tasks", cursors.ErrNotFound))
	if !ok || missing.status != http.StatusNotFound || missing.detail != "there is no stream tasks" {
		t.Errorf("not found = %#v, %v, want 404 naming the stream", missing, ok)
	}
	if _, ok := cursorFailure(errors.New("disk")); ok {
		t.Error("a server failure must not map to a problem")
	}
}

func TestOrProblemPrefersTheProblem(t *testing.T) {
	t.Parallel()
	p := failure(http.StatusServiceUnavailable, "x")
	got, err := orProblem[ListNotesResponseObject](&p, nil)
	if err != nil || got == nil {
		t.Errorf("problem = %#v, %v", got, err)
	}
	boom := errors.New("boom")
	if got, err := orProblem[ListNotesResponseObject](nil, boom); got != nil || !errors.Is(err, boom) {
		t.Errorf("error = %#v, %v", got, err)
	}
}
