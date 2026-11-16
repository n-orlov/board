package server

import (
	"context"
	"net/http"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/auth"
)

// askRaised is the 201 body of a raise: the ask as GET /api/v1/asks/{ref} gives
// it, with the lease a lease_action of keep or stop acted on.
func askRaised(a Ask, lease *Lease) AskRaised {
	out := AskRaised{
		Answer: a.Answer, AnsweredAt: a.AnsweredAt, AnsweredBy: a.AnsweredBy, Asker: a.Asker, AskerWaiting: a.AskerWaiting,
		Blocking: a.Blocking, CreatedAt: a.CreatedAt, Details: a.Details, EndedAt: a.EndedAt, Id: a.Id, Initiative: a.Initiative,
		Lease: lease, Mode: AskRaisedMode(a.Mode), OnBehalfOf: a.OnBehalfOf, Options: a.Options, Question: a.Question,
		Ref: a.Ref, State: AskRaisedState(a.State), Task: a.Task, WithdrawReason: a.WithdrawReason, WithdrawnAt: a.WithdrawnAt,
	}
	if a.Channel != nil {
		c := AskRaisedChannel(*a.Channel)
		out.Channel = &c
	}
	return out
}

// acts says the raise asks to end or keep the caller's lease, which needs the
// stores of a check-in behind it.
func acts(b *AskRaise) bool {
	return b.LeaseAction != nil && *b.LeaseAction != AskRaiseLeaseActionNone
}

// blockReady answers 503 when a lease action has no lease store behind it.
func (o askOps) blockReady() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "acting on a lease with an ask is not available"), o.block.Leases == nil || o.block.Asks == nil
}

// raiseActing raises the ask and ends or keeps the caller's live lease on its task
// (lease_action stop or keep), in one transaction (checkin.Store.Block).
func (o askOps) raiseActing(ctx context.Context, who auth.Identity, in asks.New) (RaiseAskResponseObject, error) {
	if r, off := o.blockReady(); off {
		return r, nil
	}
	res, err := o.block.Block(ctx, who.AuditActor(), who.Principal, in)
	if err != nil {
		if p, is := leaseFailure(err); is {
			return p, nil
		}
		return askResult[RaiseAskResponseObject](err)
	}
	var lease *Lease
	if res.Lease != nil {
		l := apiLease(*res.Lease)
		lease = &l
	}
	return RaiseAsk201JSONResponse(askRaised(o.apiAsk(res.Ask), lease)), nil
}
