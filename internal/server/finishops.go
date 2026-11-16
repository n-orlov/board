package server

import (
	"context"
	"net/http"

	"github.com/n-orlov/village-hall/internal/checkin"
)

func (p problemResponse) VisitFinishResponse(w http.ResponseWriter) error { return p.write(w) }

// finishOps is POST /api/v1/finish (SPEC 8.4). It is built from the other
// operations' stores once every option has been applied; without all of them it
// answers 503.
type finishOps struct {
	checkin checkin.Store
}

func newFinishOps(o operations) finishOps {
	return finishOps{checkin: newStartOps(o).checkin}
}

func (o finishOps) ready() (problemResponse, bool) {
	c := o.checkin
	off := c.Leases == nil || c.Notes == nil || c.Tasks == nil
	return failure(http.StatusServiceUnavailable, "the finish is not available"), off
}

const nextFinishBody = "Send a JSON body such as {\"outcome\": \"what you did\"}, with \"task\" to name the task and \"done\": true to mark it done."

// Finish ends the caller's live lease, writes the outcome as a note on the task
// and, with done, marks the task done, in one transaction.
func (o finishOps) Finish(ctx context.Context, req FinishRequestObject) (FinishResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with an outcome").withNext(nextFinishBody), nil
	}
	res, err := o.checkin.Finish(ctx, who.AuditActor(), who.Principal, checkin.FinishRequest{
		Task: text(req.Body.Task), Outcome: req.Body.Outcome, Done: req.Body.Done != nil && *req.Body.Done,
	})
	if err != nil {
		if p, is := startFailure(err); is {
			return p, nil
		}
		return nil, err
	}
	return Finish200JSONResponse(FinishResult{Lease: apiLease(res.Lease), Note: apiNote(res.Note, false), Done: res.Done}), nil
}
