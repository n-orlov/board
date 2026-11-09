package attention

import (
	"context"
	"fmt"
	"sort"

	"github.com/n-orlov/board/internal/leases"
)

// conflicts lists the resources that two or more live leases hold, the one with
// the newest lease first. A conflict is kept when any of its leases is on a task
// the scope keeps.
func (s Store) conflicts(ctx context.Context, sc scope) ([]Conflict, error) {
	live, err := (leases.Store{Store: s.Store, Now: s.Now}).Live(ctx)
	if err != nil {
		return nil, err
	}
	kept, err := s.keptTasks(ctx, sc)
	if err != nil {
		return nil, err
	}
	wayBacks, err := s.wayBacks(ctx, live)
	if err != nil {
		return nil, err
	}
	return contested(live, kept, wayBacks), nil
}

// contested groups the leases by resource and returns the resources with two or
// more of them. kept is nil for no scope.
func contested(live []leases.Lease, kept map[string]bool, wayBacks map[string]string) []Conflict {
	byResource := map[string][]leases.Lease{}
	for _, l := range live {
		for _, r := range l.Resources {
			byResource[r] = append(byResource[r], l)
		}
	}
	out := []Conflict{}
	for resource, held := range byResource {
		if len(held) > 1 && inScope(held, kept) {
			out = append(out, conflictOf(resource, held, wayBacks))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if ni, nj := newest(byResource[out[i].Resource]), newest(byResource[out[j].Resource]); ni != nj {
			return ni > nj
		}
		return out[i].Resource < out[j].Resource
	})
	return out
}

func inScope(held []leases.Lease, kept map[string]bool) bool {
	if kept == nil {
		return true
	}
	for _, l := range held {
		if kept[l.TaskID] {
			return true
		}
	}
	return false
}

// newest is the highest lease number among held.
func newest(held []leases.Lease) int64 {
	var n int64
	for _, l := range held {
		n = max(n, l.N)
	}
	return n
}

// conflictOf is the conflict on resource that the leases held hold, in lease-number order.
func conflictOf(resource string, held []leases.Lease, wayBacks map[string]string) Conflict {
	c := Conflict{Resource: resource, Holders: []Holder{}, Tasks: []string{}}
	seen := map[string]bool{}
	for _, l := range held {
		c.Holders = append(c.Holders, Holder{Lease: l.Ref, Actor: Actor{Handle: l.Actor, WayBack: wayBacks[l.Actor]},
			Task: l.Task, Intent: l.Intent, ExpiresAt: l.ExpiresAt})
		if !seen[l.Task] {
			seen[l.Task] = true
			c.Tasks = append(c.Tasks, l.Task)
		}
	}
	return c
}

// keptTasks is the set of ids of the tasks the scope keeps, nil for an empty scope.
func (s Store) keptTasks(ctx context.Context, sc scope) (map[string]bool, error) {
	if sc.empty() {
		return nil, nil
	}
	cond, args := sc.and(`t.initiative_id`)
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(`SELECT t.id FROM tasks t WHERE 1 = 1`+cond), args...)
	if err != nil {
		return nil, fmt.Errorf("read attention scope: %w", err)
	}
	defer func() { _ = rows.Close() }()
	kept := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read attention scope: %w", err)
		}
		kept[id] = true
	}
	return kept, rows.Err()
}

// wayBacks maps the handle of each holder of live to the way back on record.
func (s Store) wayBacks(ctx context.Context, live []leases.Lease) (map[string]string, error) {
	out := map[string]string{}
	if len(live) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(live))
	for _, l := range live {
		ids = append(ids, l.ActorID)
	}
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(
		`SELECT handle, COALESCE(way_back, '') FROM actors WHERE id IN (`+marks(len(ids))+`)`), anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("read attention actors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var handle, wayBack string
		if err := rows.Scan(&handle, &wayBack); err != nil {
			return nil, fmt.Errorf("read attention actors: %w", err)
		}
		out[handle] = wayBack
	}
	return out, rows.Err()
}
