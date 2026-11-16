package server

import (
	"context"
	"net/http"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/checkin"
)

func (p problemResponse) VisitCatchupResponse(w http.ResponseWriter) error { return p.write(w) }

// catchupOps is POST /api/v1/catchup (SPEC 8.4). It is built from the other
// operations' stores once every option has been applied; without all of them it
// answers 503.
type catchupOps struct {
	checkin checkin.Store
}

func newCatchupOps(o operations) catchupOps {
	return catchupOps{checkin: newStartOps(o).checkin}
}

func (o catchupOps) ready() (problemResponse, bool) {
	c := o.checkin
	off := c.Cursors == nil || c.Notes == nil || c.Asks == nil
	return failure(http.StatusServiceUnavailable, "the catch-up is not available"), off
}

// Catchup answers the notes after the caller's notes cursor, moving the cursor
// to the last of them, and the answers to its asks since its previous catch-up.
func (o catchupOps) Catchup(ctx context.Context, _ CatchupRequestObject) (CatchupResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	res, err := o.checkin.Catchup(ctx, who.AuditActor(), who.Principal)
	if err != nil {
		return nil, err
	}
	out := CatchupResult{Notes: make([]Note, len(res.Notes)), NotesMore: res.NotesMore, Answers: make([]Ask, len(res.Answers))}
	for i, k := range res.Notes {
		out.Notes[i] = apiNote(k, false)
	}
	for i, k := range res.Answers {
		out.Answers[i] = askOps{asks: &asks.Store{}}.apiAsk(k)
	}
	return Catchup200JSONResponse(out), nil
}
