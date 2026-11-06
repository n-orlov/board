package leases

import (
	"context"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Now is the lease and conflict parts of an actor's check-in (SPEC 6.5): the
// actor's live leases, those of the others, and the conflicts.
type Now struct {
	Mine      []Lease
	Others    []Lease
	Conflicts []domain.Holding
}

// Live lists every live lease whose expires_at is not before now, oldest first.
// Like LiveFor it leaves out the leases that have run out without expiring them.
func (s Store) Live(ctx context.Context) ([]Lease, error) {
	return s.readLeases(ctx, selectLease+` WHERE l.state = 'live' AND l.expires_at >= ? ORDER BY l.n`,
		[]any{store.EncodeTime(s.now())})
}

// CheckIn answers the check-in of the actor with the handle me. It expires the due
// leases first (each with its one lease_expired event), so no lease it lists is
// past its expiry. queried are resources to check before starting: the live
// leases of others that hold one are conflicts too (domain.NowConflicts). Every
// list is empty, never nil.
func (s Store) CheckIn(ctx context.Context, me string, queried []string) (Now, error) {
	if _, err := s.ExpireDue(ctx); err != nil {
		return Now{}, err
	}
	live, err := s.Live(ctx)
	if err != nil {
		return Now{}, err
	}
	out := Now{Mine: []Lease{}, Others: []Lease{}}
	holdings := make([]domain.Holding, len(live))
	for i, l := range live {
		holdings[i] = l.Holding()
		if l.Actor == me {
			out.Mine = append(out.Mine, l)
		} else {
			out.Others = append(out.Others, l)
		}
	}
	out.Conflicts = domain.NowConflicts(me, holdings, queried)
	return out, nil
}
