package server

import (
	"context"
	"net/http"

	"github.com/n-orlov/village-hall/internal/checkin"
)

func (p problemResponse) VisitPingResponse(w http.ResponseWriter) error { return p.write(w) }

// pingOps is POST /api/v1/ping (SPEC 8.4). It is built from the other
// operations' stores once every option has been applied; without all of them it
// answers 503.
type pingOps struct {
	checkin checkin.Store
}

func newPingOps(o operations) pingOps {
	return pingOps{checkin: newStartOps(o).checkin}
}

func (o pingOps) ready() (problemResponse, bool) {
	c := o.checkin
	off := c.Leases == nil || c.Tasks == nil
	return failure(http.StatusServiceUnavailable, "the ping is not available"), off
}

// Ping renews every live lease the caller holds and, with progress, writes one
// milestone on each leased task, in one transaction. The body is optional.
func (o pingOps) Ping(ctx context.Context, req PingRequestObject) (PingResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	var in checkin.PingRequest
	if req.Body != nil {
		in.Progress = req.Body.Progress
	}
	res, err := o.checkin.Ping(ctx, who.AuditActor(), who.Principal, in)
	if err != nil {
		if p, is := startFailure(err); is {
			return p, nil
		}
		return nil, err
	}
	out := PingResult{Leases: make([]Lease, len(res.Leases)), Milestones: res.Milestones}
	if out.Milestones == nil {
		out.Milestones = []string{}
	}
	for i, l := range res.Leases {
		out.Leases[i] = apiLease(l)
	}
	return Ping200JSONResponse(out), nil
}
