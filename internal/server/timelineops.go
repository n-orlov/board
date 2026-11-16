package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/timeline"
)

func (p problemResponse) VisitGetTimelineResponse(w http.ResponseWriter) error { return p.write(w) }

// timelineOps is GET /api/v1/timeline (SPEC 6.10). Any authenticated principal
// may use it: v1 has no ACL.
type timelineOps struct {
	timeline *timeline.Store
	// leases is expired before the read, so no lease is shown live past its expiry.
	leases *leases.Store
	// reasons derives the attention list of each task; nil means none needs a person.
	derived *attention.Store
}

// ready answers 503 when the operation has no store behind it.
func (o timelineOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "the timeline is not available"), o.timeline == nil || o.leases == nil
}

// timelineQuery is the store's query for the parameters of GET /api/v1/timeline.
func timelineQuery(p GetTimelineParams) timeline.Query {
	return timeline.Query{
		From: text(p.From), To: text(p.To), Initiatives: orEmpty(p.Initiative), Tags: orEmpty(p.Tag),
		Actor: text(p.Actor), ClosedWithin: text(p.ClosedWithin), Rows: rowsText(p.Rows),
	}
}

// timelineFailure is the problem a failed read is answered with, or false when
// the failure is the server's.
func timelineFailure(err error) (problemResponse, bool) {
	var bad timeline.InvalidError
	switch {
	case errors.As(err, &bad):
		return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), true
	case errors.Is(err, timeline.ErrNoInitiative):
		return failure(http.StatusNotFound, "there is no initiative "+afterColon(err)).
			withNext("Send the slug of an initiative from GET /api/v1/initiatives."), true
	}
	return problemResponse{}, false
}

// GetTimeline reads the tasks, leases and markers of a window.
func (o timelineOps) GetTimeline(ctx context.Context, req GetTimelineRequestObject) (GetTimelineResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	// A refusal comes before the expiry below, which writes: a refused request changes nothing.
	if p, bad := o.refusal(ctx, req.Params); bad {
		return p, nil
	}
	if _, err := o.leases.ExpireDue(ctx); err != nil {
		return nil, err
	}
	got, err := o.timeline.Read(ctx, timelineQuery(req.Params))
	if p, ok := timelineFailure(err); ok {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	doc, err := o.document(ctx, got)
	if err != nil {
		return nil, err
	}
	return GetTimeline200JSONResponse(doc), nil
}

// document is the answer for a read, with each task's attention list from the
// attention derivation.
func (o timelineOps) document(ctx context.Context, got timeline.Result) (Timeline, error) {
	var reasons attention.Derivation
	if o.derived != nil {
		var err error
		if reasons, err = o.derived.Reasons(ctx, attention.Query{}); err != nil {
			return Timeline{}, err
		}
	}
	out := Timeline{
		From: got.From, To: got.To, Initiatives: make([]TimelineInitiative, len(got.Initiatives)),
		Tasks: make([]TimelineTask, len(got.Tasks)), Leases: make([]TimelineLease, len(got.Leases)), Markers: make([]TimelineMarker, len(got.Markers)),
	}
	for i, v := range got.Initiatives {
		out.Initiatives[i] = TimelineInitiative{Slug: v.Slug, Title: v.Title, Tags: v.Tags, Closed: v.Closed}
	}
	for i, t := range got.Tasks {
		out.Tasks[i] = apiTimelineTask(t)
		out.Tasks[i].Attention, _ = apiTaskAttention(reasons.Of(t.Ref))
	}
	for i, l := range got.Leases {
		out.Leases[i] = TimelineLease{Ref: l.Ref, Task: l.Task, Holder: l.Holder, State: TimelineLeaseState(l.State),
			StartedAt: l.StartedAt, EndedAt: l.EndedAt, Intent: l.Intent}
	}
	for i, m := range got.Markers {
		out.Markers[i] = TimelineMarker{Task: m.Task, Type: TimelineMarkerType(m.Type), At: m.At, Label: m.Label}
	}
	return out, nil
}

func apiTimelineTask(t timeline.Task) TimelineTask {
	return TimelineTask{
		Ref: t.Ref, Initiative: t.Initiative, Title: t.Title, Status: TimelineTaskStatus(t.Status), Kind: TimelineTaskKind(t.Kind),
		PlannedStart: t.PlannedStart, PlannedEnd: t.PlannedEnd, CreatedAt: t.CreatedAt, ClosedAt: t.ClosedAt, Deps: t.Deps,
	}
}

// rowsText is the text of the rows parameter, empty when it is left out.
func rowsText(r *GetTimelineParamsRows) string {
	if r == nil {
		return ""
	}
	return string(*r)
}

// refusal is the problem a request that cannot be read is answered with, or
// false when it can. It is decided before anything is written: a rows value
// that was supplied and is neither window nor all (the empty value included), a
// window, closed_within or tag that does not read, or an initiative that names
// nothing. A request that leaves rows out is never refused for it.
func (o timelineOps) refusal(ctx context.Context, p GetTimelineParams) (problemResponse, bool) {
	if p.Rows != nil {
		if r, bad := timelineFailure(timeline.ValidRows(string(*p.Rows))); bad {
			return r, true
		}
	}
	err := o.timeline.Check(ctx, timelineQuery(p))
	if r, bad := timelineFailure(err); bad {
		return r, true
	}
	return problemResponse{}, false
}
