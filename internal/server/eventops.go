package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/n-orlov/village-hall/internal/cursors"
	"github.com/n-orlov/village-hall/internal/events"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/tasks"
)

func (p problemResponse) VisitListEventsResponse(w http.ResponseWriter) error     { return p.write(w) }
func (p problemResponse) VisitListTaskEventsResponse(w http.ResponseWriter) error { return p.write(w) }

// eventOps is the event reads: GET /api/v1/events and GET
// /api/v1/tasks/{ref}/events (SPEC 6.2, 6.4). Any authenticated principal may
// use them: v1 has no ACL.
type eventOps struct {
	events  *events.Store
	cursors cursorOps
}

// ready answers 503 when the operations have no store behind them.
func (o eventOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "events are not available"), o.events == nil
}

// eventFailure is the problem a failed event read is answered with, or false
// when the failure is the server's.
func eventFailure(err error) (problemResponse, bool) {
	var bad events.InvalidError
	switch {
	case errors.As(err, &bad):
		return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), true
	case errors.Is(err, tasks.ErrNotFound):
		return failure(http.StatusNotFound, "there is no task "+afterColon(err)).
			withNext("Read a task with GET /api/v1/tasks/<ref>, using a ref like <initiative>-<number> or a ULID."), true
	case errors.Is(err, initiatives.ErrNotFound):
		return failure(http.StatusNotFound, "there is no initiative "+afterColon(err)).
			withNext("Send the slug of an initiative from GET /api/v1/initiatives."), true
	}
	return problemResponse{}, false
}

// eventResult answers a failed event read: the problem it maps to, or the error
// itself when it is the server's.
func eventResult[R any](err error) (R, error) {
	var zero R
	if p, ok := eventFailure(err); ok {
		// problemResponse implements every Visit method of the event operations
		if r, is := any(p).(R); is {
			return r, nil
		}
	}
	return zero, err
}

func apiEvent(e events.Event) Event {
	return Event{
		Id: e.ID, Ref: e.Ref, At: e.At, Type: e.Type, Actor: e.Actor, Principal: e.Principal, Origin: optionalText(e.Origin),
		Initiative: optionalText(e.Initiative), Task: optionalText(e.Task), RefId: optionalText(e.RefID),
		Summary: e.Summary, Data: e.Data,
	}
}

func apiEventList(p events.Page) EventList {
	items := make([]Event, len(p.Items))
	for i, e := range p.Items {
		items[i] = apiEvent(e)
	}
	return EventList{Items: items, NextAfter: optionalText(p.NextAfter)}
}

// pageLimit is the store's limit for an optional limit parameter: an explicit 0
// is out of range, not the default.
func pageLimit(limit *EventLimit) int {
	if limit == nil {
		return 0
	}
	if *limit == 0 {
		return -1
	}
	return *limit
}

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// eventQuery is the store's query for the parameters of GET /api/v1/events.
func eventQuery(p ListEventsParams) events.Query {
	q := events.Query{
		Since: text(p.Since), After: text(p.After), Initiative: text(p.Initiative), Task: text(p.Task),
		Actor: text(p.Actor), Limit: pageLimit(p.Limit),
	}
	if p.Type != nil {
		q.Types = *p.Type
	}
	return q
}

// ListEvents lists a page of events. `since=cursor` reads after the caller's
// events cursor.
func (o eventOps) ListEvents(ctx context.Context, req ListEventsRequestObject) (ListEventsResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	since, problem, err := o.cursors.readFrom(ctx, cursors.StreamEvents, text(req.Params.Since))
	if problem != nil || err != nil {
		return orProblem[ListEventsResponseObject](problem, err)
	}
	q := eventQuery(req.Params)
	q.Since = since
	page, err := o.events.List(ctx, q)
	if err != nil {
		return eventResult[ListEventsResponseObject](err)
	}
	return ListEvents200JSONResponse(apiEventList(page)), nil
}

// ListTaskEvents lists a page of the events of one task.
func (o eventOps) ListTaskEvents(ctx context.Context, req ListTaskEventsRequestObject) (ListTaskEventsResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	page, err := o.events.ForTask(ctx, req.Ref, text(req.Params.After), pageLimit(req.Params.Limit))
	if err != nil {
		return eventResult[ListTaskEventsResponseObject](err)
	}
	return ListTaskEvents200JSONResponse(apiEventList(page)), nil
}
