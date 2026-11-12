package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/n-orlov/board/internal/roster"
)

func (p problemResponse) VisitListActorsResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitGetActorResponse(w http.ResponseWriter) error   { return p.write(w) }

// actorOps is GET /api/v1/actors and GET /api/v1/actors/{handle} (SPEC 6.11).
// Any authenticated principal may use them: v1 has no ACL.
type actorOps struct{ roster *roster.Store }

// ready answers 503 when the operations have no store behind them.
func (o actorOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "actors are not available"), o.roster == nil
}

func apiActorView(a roster.Actor) ActorView {
	out := ActorView{
		Handle: a.Handle, Kind: ActorViewKind(a.Kind), WayBack: optionalText(a.WayBack), LastSeen: a.LastSeen,
		Leases: make([]ActorLease, len(a.Leases)), Asks: make([]ActorAsk, len(a.Asks)),
	}
	for i, l := range a.Leases {
		out.Leases[i] = ActorLease{Ref: l.Ref, Task: l.Task, Intent: l.Intent, ExpiresAt: l.ExpiresAt, Resources: l.Resources}
	}
	for i, k := range a.Asks {
		out.Asks[i] = ActorAsk{Ref: k.Ref, Task: optionalText(k.Task), Initiative: optionalText(k.Initiative), Question: k.Question,
			Mode: ActorAskMode(k.Mode), Blocking: k.Blocking, CreatedAt: k.CreatedAt}
	}
	return out
}

// ListActors lists the actors, the one seen last first.
func (o actorOps) ListActors(ctx context.Context, req ListActorsRequestObject) (ListActorsResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	got, err := o.roster.List(ctx, ptrOr(req.Params.Alive, false))
	if err != nil {
		return nil, err
	}
	items := make([]ActorView, len(got))
	for i, a := range got {
		items[i] = apiActorView(a)
	}
	return ListActors200JSONResponse(ActorList{Items: items}), nil
}

// GetActor reads one actor by handle.
func (o actorOps) GetActor(ctx context.Context, req GetActorRequestObject) (GetActorResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	a, err := o.roster.Get(ctx, req.Handle)
	if errors.Is(err, roster.ErrNotFound) {
		return failure(http.StatusNotFound, "there is no actor "+afterColon(err)).
			withNext("Send the handle of an actor from GET /api/v1/actors."), nil
	}
	if err != nil {
		return nil, err
	}
	return GetActor200JSONResponse(apiActorView(a)), nil
}
