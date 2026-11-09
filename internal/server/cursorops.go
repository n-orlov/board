package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/n-orlov/board/internal/cursors"
)

func (p problemResponse) VisitPutCursorResponse(w http.ResponseWriter) error { return p.write(w) }

// sinceCursor is the value of `since` that reads from the caller's cursor.
const sinceCursor = "cursor"

// cursorOps is PUT /api/v1/cursors/{stream} (SPEC 6.7). Any authenticated
// principal may use it: v1 has no ACL. The note and event reads share its
// store to resolve `since=cursor`.
type cursorOps struct{ cursors *cursors.Store }

// ready answers 503 when the operations have no store behind them.
func (o cursorOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "cursors are not available"), o.cursors == nil
}

const nextCursorStream = "Use the stream notes or events."

// cursorFailure is the problem a failed cursor operation is answered with, or
// false when the failure is the server's.
func cursorFailure(err error) (problemResponse, bool) {
	var bad cursors.InvalidError
	switch {
	case errors.As(err, &bad):
		return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), true
	case errors.Is(err, cursors.ErrNotFound):
		return failure(http.StatusNotFound, "there is no "+afterColon(err)).withNext(nextCursorStream), true
	}
	return problemResponse{}, false
}

// PutCursor sets the caller's cursor on a stream.
func (o cursorOps) PutCursor(ctx context.Context, req PutCursorRequestObject) (PutCursorResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with a position").
			withNext("Send {\"position\": \"N40\"} for notes or {\"position\": \"E42\"} for events."), nil
	}
	c, err := o.cursors.Set(ctx, who.AuditActor(), req.Stream, req.Body.Position)
	if err != nil {
		if p, known := cursorFailure(err); known {
			return p, nil
		}
		return nil, err
	}
	return PutCursor200JSONResponse(Cursor{Stream: c.Stream, Position: c.Position, Ref: c.Ref}), nil
}

// readFrom turns a `since` value into what the stores read: `cursor` becomes
// the caller's position on the stream (empty when none is set, which reads from
// the start) and any other value is passed through. The problem is the answer
// when the cursor cannot be read.
func (o cursorOps) readFrom(ctx context.Context, stream string, since string) (string, *problemResponse, error) {
	if since != sinceCursor {
		return since, nil, nil
	}
	if r, off := o.ready(); off {
		return "", &r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return "", &r, nil
	}
	position, err := o.cursors.Get(ctx, who.Actor, stream)
	return position, nil, err
}

// orProblem answers a read whose cursor could not be used: the problem when
// there is one, else the error itself.
func orProblem[R any](problem *problemResponse, err error) (R, error) {
	var zero R
	if problem != nil {
		// problemResponse implements every Visit method of the operations that call this
		if r, is := any(*problem).(R); is {
			return r, nil
		}
	}
	return zero, err
}
