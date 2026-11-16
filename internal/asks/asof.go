package asks

import (
	"time"

	"github.com/n-orlov/village-hall/internal/domain"
)

// AsOf is k as it stood at the instant at, by its commit mark alone: k itself
// when it is open, or when its end's mark (EndCommitAt, read after the end's
// COMMIT returned) is at or before at, which proves the end committed by then.
// Any other end is returned still open, with every field its end set cleared:
// one marked after at, and one without a mark, however early its answered_at
// or withdrawn_at and however late it is read. Evidence that is not a mark (a
// read of the ask that came back by at) is the caller's to supply (SPEC 4.5, 7.2).
func (k Ask) AsOf(at time.Time) Ask {
	if k.State == domain.AskOpen || (k.EndCommitAt != nil && !k.EndCommitAt.After(at)) {
		return k
	}
	k.State = domain.AskOpen
	k.Answer, k.AnsweredBy, k.OnBehalfOf, k.Channel, k.WithdrawReason = "", "", "", "", ""
	k.AnsweredAt, k.WithdrawnAt, k.EndedAt, k.EndCommitAt = nil, nil, nil, nil
	return k
}
