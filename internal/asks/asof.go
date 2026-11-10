package asks

import (
	"time"

	"github.com/n-orlov/board/internal/domain"
)

// AsOf is k as it stood at the instant at: k itself, unless k was answered or
// withdrawn by a transaction whose commit mark (EndCommitAt) is at or after at,
// which had therefore not committed by then. That k is returned still open,
// with every field its end set cleared, however early its answered_at or
// withdrawn_at and however late it is read. An end without a mark is taken as
// it reads (SPEC 7.2).
func (k Ask) AsOf(at time.Time) Ask {
	if k.State == domain.AskOpen || k.EndCommitAt == nil || k.EndCommitAt.Before(at) {
		return k
	}
	k.State = domain.AskOpen
	k.Answer, k.AnsweredBy, k.OnBehalfOf, k.Channel, k.WithdrawReason = "", "", "", "", ""
	k.AnsweredAt, k.WithdrawnAt, k.EndedAt, k.EndCommitAt = nil, nil, nil, nil
	return k
}
