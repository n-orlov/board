package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/n-orlov/board/internal/actors"
	"github.com/n-orlov/board/internal/audit"
)

func (p problemResponse) VisitGetMetaResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitPutProtocolSeenResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// Build is what the server reports about itself: the version it was built as
// and the agent protocol version its binary embeds (SPEC 8.6).
type Build struct {
	Version  string
	Protocol int
}

// metaOps is GET /api/v1/meta and PUT /api/v1/actors/me/protocol-seen. Any
// authenticated principal may use them: v1 has no ACL.
type metaOps struct {
	build  Build
	actors *actors.Store
}

// GetMeta describes the server and the caller. It reads nothing from the
// database.
func (o metaOps) GetMeta(ctx context.Context, _ GetMetaRequestObject) (GetMetaResponseObject, error) {
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	return GetMeta200JSONResponse(Meta{Version: o.build.Version, Protocol: o.build.Protocol, Principal: who.Principal, Actor: who.Actor}), nil
}

// PutProtocolSeen records that the calling actor has read a protocol version.
func (o metaOps) PutProtocolSeen(ctx context.Context, req PutProtocolSeenRequestObject) (PutProtocolSeenResponseObject, error) {
	if o.actors == nil {
		return failure(http.StatusServiceUnavailable, "actors are not available"), nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with a version").
			withNext("Send {\"version\": N}, the number 'board help agent --version' prints."), nil
	}
	seen, err := o.actors.RecordProtocolSeen(ctx, audit.Actor{Handle: who.Actor, Operator: who.Operator}, req.Body.Version)
	switch {
	case errors.Is(err, actors.ErrNegativeVersion):
		return failure(http.StatusBadRequest, err.Error()).
			withNext("Send {\"version\": N}, the number 'board help agent --version' prints."), nil
	case err != nil:
		return nil, err
	}
	return PutProtocolSeen200JSONResponse(ProtocolSeen{Actor: who.Actor, ProtocolSeen: seen}), nil
}
