package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/cursors"
	"github.com/n-orlov/board/internal/notes"
)

func (p problemResponse) VisitPostNoteResponse(w http.ResponseWriter) error  { return p.write(w) }
func (p problemResponse) VisitListNotesResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitGetNoteResponse(w http.ResponseWriter) error   { return p.write(w) }

// noteOps is the /api/v1/notes operations (SPEC 6.7). Any authenticated
// principal may use them: v1 has no ACL.
type noteOps struct {
	notes   *notes.Store
	cursors cursorOps
}

// ready answers 503 when the operations have no store behind them.
func (o noteOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "notes are not available"), o.notes == nil
}

const (
	nextNoteBody = "Send a JSON body such as {\"summary\": \"found the cause\"}, with a task or an initiative if the note belongs to one."
	nextNoteRead = "Read the notes with GET /api/v1/notes."
)

// noteFailure is the problem a failed note operation is answered with, or false
// when the failure is the server's.
func noteFailure(err error) (problemResponse, bool) {
	var bad notes.InvalidError
	if errors.As(err, &bad) {
		return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), true
	}
	if !errors.Is(err, notes.ErrNotFound) {
		return problemResponse{}, false
	}
	subject := afterColon(err)
	switch {
	case strings.HasPrefix(subject, "task "):
		return failure(http.StatusNotFound, "there is no "+subject).
			withNext("Read a task with GET /api/v1/tasks/<ref>, using a ref like <initiative>-<number> or a ULID."), true
	case strings.HasPrefix(subject, "initiative "):
		return failure(http.StatusNotFound, "there is no "+subject).
			withNext("Send the slug of an initiative from GET /api/v1/initiatives."), true
	}
	return failure(http.StatusNotFound, "there is no "+subject).withNext(nextNoteRead), true
}

// noteResult answers a failed note operation: the problem it maps to, or the
// error itself when it is the server's.
func noteResult[R any](err error) (R, error) {
	var zero R
	if p, ok := noteFailure(err); ok {
		// problemResponse implements every Visit method of the note operations
		if r, is := any(p).(R); is {
			return r, nil
		}
	}
	return zero, err
}

// apiNote is the note document. The body is left out unless the read asked for
// it; has_body tells whether the note has one.
func apiNote(k notes.Note, withBody bool) Note {
	out := Note{
		Id: k.ID, Ref: k.Ref, Author: k.Actor, Summary: k.Summary, HasBody: k.HasBody, Task: optionalText(k.Task),
		Initiative: optionalText(k.Initiative), Resource: optionalText(k.Resource), Mentions: k.Mentions,
		Broadcast: k.Broadcast, CreatedAt: k.CreatedAt,
	}
	if withBody && k.HasBody {
		out.Body = &k.Body
	}
	if out.Mentions == nil {
		out.Mentions = []string{}
	}
	return out
}

func noteRequest(b *NotePost) notes.New {
	in := notes.New{Task: text(b.Task), Initiative: text(b.Initiative), Summary: b.Summary, Body: text(b.Body), Resource: text(b.Resource)}
	if b.Mentions != nil {
		in.Mentions = *b.Mentions
	}
	return in
}

// PostNote posts a note.
func (o noteOps) PostNote(ctx context.Context, req PostNoteRequestObject) (PostNoteResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	return o.post(ctx, who, req)
}

func (o noteOps) post(ctx context.Context, who auth.Identity, req PostNoteRequestObject) (PostNoteResponseObject, error) {
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with a summary").withNext(nextNoteBody), nil
	}
	k, err := o.notes.Post(ctx, who.AuditActor(), who.Principal, noteRequest(req.Body))
	if err != nil {
		return noteResult[PostNoteResponseObject](err)
	}
	return PostNote201JSONResponse(apiNote(k, true)), nil
}

// GetNote reads one note with its body.
func (o noteOps) GetNote(ctx context.Context, req GetNoteRequestObject) (GetNoteResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	k, err := o.notes.Get(ctx, req.Ref)
	if err != nil {
		return noteResult[GetNoteResponseObject](err)
	}
	return GetNote200JSONResponse(apiNote(k, true)), nil
}

// fieldsFull reads the fields parameter: summary (the default) or full.
func fieldsFull(fields *string) (bool, *notes.InvalidError) {
	switch text(fields) {
	case "", "summary":
		return false, nil
	case "full":
		return true, nil
	}
	return false, &notes.InvalidError{Msg: "fields " + text(fields) + " is not summary or full",
		Next: "Send fields=summary (the default) or fields=full."}
}

func noteQuery(p ListNotesParams, full bool) notes.Query {
	q := notes.Query{
		Since: text(p.Since), After: text(p.After), Task: text(p.Task), Initiative: text(p.Initiative),
		Broadcast: text(p.Broadcast), Mention: text(p.Mention), Author: text(p.Author), Full: full, Limit: pageLimit(p.Limit),
	}
	if p.Resource != nil {
		q.Resources = *p.Resource
	}
	return q
}

// ListNotes lists a page of notes. `since=cursor` reads after the caller's
// notes cursor and `ack=true` moves that cursor to the last note returned.
func (o noteOps) ListNotes(ctx context.Context, req ListNotesRequestObject) (ListNotesResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	full, bad := fieldsFull(req.Params.Fields)
	if bad != nil {
		return noteResult[ListNotesResponseObject](*bad)
	}
	ack := req.Params.Ack != nil && *req.Params.Ack
	who, r, ok := caller(ctx)
	if !ok && ack {
		return r, nil
	}
	if r, off := o.cursors.ready(); off && ack {
		return r, nil
	}
	since, problem, err := o.cursors.readFrom(ctx, cursors.StreamNotes, text(req.Params.Since))
	if problem != nil || err != nil {
		return orProblem[ListNotesResponseObject](problem, err)
	}
	q := noteQuery(req.Params, full)
	q.Since = since
	page, err := o.notes.List(ctx, q)
	if err != nil {
		return noteResult[ListNotesResponseObject](err)
	}
	if ack && len(page.Items) > 0 {
		if err := o.cursors.cursors.Ack(ctx, who.AuditActor(), cursors.StreamNotes, page.Items[len(page.Items)-1].ID); err != nil {
			return nil, err
		}
	}
	items := make([]Note, len(page.Items))
	for i, k := range page.Items {
		items[i] = apiNote(k, full)
	}
	return ListNotes200JSONResponse(NoteList{Items: items, NextAfter: optionalText(page.NextAfter)}), nil
}
