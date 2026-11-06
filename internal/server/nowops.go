package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/leases"
)

func (p problemResponse) VisitGetNowResponse(w http.ResponseWriter) error { return p.write(w) }

const nextNowResources = "Send at most 20 comma-separated resources of 1 to 100 characters, such as ?resources=repo:board,host:build1."

// queriedResources reads the comma-separated resources query: none for an absent
// or empty value, otherwise each entry validated like a lease's resources.
func queriedResources(raw *string) ([]string, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	return domain.NormalizeResources(strings.Split(*raw, ","))
}

func apiNowLease(l leases.Lease) NowLease {
	a := apiLease(l)
	return NowLease{
		Id: a.Id, Ref: a.Ref, Task: a.Task, Title: l.Title, Actor: a.Actor, Intent: a.Intent, TtlSeconds: a.TtlSeconds,
		StartedAt: a.StartedAt, RenewedAt: a.RenewedAt, ExpiresAt: a.ExpiresAt, State: NowLeaseState(a.State),
		Resources: a.Resources, EndedAt: a.EndedAt, EndedBy: a.EndedBy, Outcome: a.Outcome, AckedAt: a.AckedAt, AckedBy: a.AckedBy,
	}
}

func apiNow(actor string, n leases.Now) Now {
	out := Now{
		Actor: actor, MyLeases: make([]NowLease, len(n.Mine)), OthersLeases: make([]Lease, len(n.Others)),
		Conflicts: make([]LeaseConflict, len(n.Conflicts)),
	}
	for i, l := range n.Mine {
		out.MyLeases[i] = apiNowLease(l)
	}
	for i, l := range n.Others {
		out.OthersLeases[i] = apiLease(l)
	}
	for i, h := range n.Conflicts {
		out.Conflicts[i] = apiConflict(h)
	}
	return out
}

// GetNow answers the caller's check-in: its live leases, those of the others and
// the conflicts (SPEC 6.5). It writes nothing but the lease_expired events of the
// leases it expires.
func (o leaseOps) GetNow(ctx context.Context, req GetNowRequestObject) (GetNowResponseObject, error) {
	who, bad := o.writer(ctx)
	if bad != nil {
		return *bad, nil
	}
	queried, err := queriedResources(req.Params.Resources)
	if err != nil {
		return failure(http.StatusBadRequest, err.Error()).withNext(nextNowResources), nil
	}
	n, err := o.leases.CheckIn(ctx, who.Actor, queried)
	if err != nil {
		return nil, err
	}
	return GetNow200JSONResponse(apiNow(who.Actor, n)), nil
}
