package roster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/store"
)

// ErrNotFound is a handle that names no actor.
var ErrNotFound = errors.New("actor not found")

// DefaultPage is how many leases or asks one read of the lists asks for.
const DefaultPage = 1000

// Actor is an actor with its live leases, oldest first, and its open asks in
// order of raising. Both are empty, never nil.
type Actor struct {
	Handle, Kind, WayBack string
	LastSeen              time.Time
	Leases                []leases.Lease
	Asks                  []asks.Ask
}

// Alive reports whether the actor holds a live lease.
func (a Actor) Alive() bool { return len(a.Leases) > 0 }

// Store reads the roster of one database.
type Store struct {
	Store  *store.Store
	Leases *leases.Store
	Asks   *asks.Store
	// Page is the page size the leases and asks are read with; 0 means DefaultPage.
	Page int
}

func (s Store) page() int {
	if s.Page > 0 {
		return s.Page
	}
	return DefaultPage
}

// List reads the actors, the one seen last first (ties by handle). With alive it
// keeps those that hold a live lease. The lease list expires the leases that ran out
// before it reads, so none is shown live past its expiry.
func (s Store) List(ctx context.Context, alive bool) ([]Actor, error) {
	out, err := s.read(ctx, "")
	if err != nil {
		return nil, err
	}
	if !alive {
		return out, nil
	}
	kept := out[:0]
	for _, a := range out {
		if a.Alive() {
			kept = append(kept, a)
		}
	}
	return kept, nil
}

// Get reads one actor by handle, or returns ErrNotFound.
func (s Store) Get(ctx context.Context, handle string) (Actor, error) {
	got, err := s.read(ctx, handle)
	if err != nil {
		return Actor{}, err
	}
	if len(got) == 0 {
		return Actor{}, fmt.Errorf("%w: %s", ErrNotFound, handle)
	}
	return got[0], nil
}

// read is the actors a handle filter keeps (all when it is empty) with their
// leases and asks attached.
func (s Store) read(ctx context.Context, handle string) ([]Actor, error) {
	list, err := s.actors(ctx, handle)
	if err != nil {
		return nil, err
	}
	liveLeases, err := pages(func(after string) ([]leases.Lease, string, error) {
		p, err := s.Leases.List(ctx, leases.Query{State: leases.StateLive, Actor: handle, After: after, Limit: s.page()})
		return p.Items, p.NextAfter, err
	})
	if err != nil {
		return nil, err
	}
	openAsks, err := pages(func(after string) ([]asks.Ask, string, error) {
		p, err := s.Asks.List(ctx, asks.Query{State: asks.StateOpen, Actor: handle, After: after, Limit: s.page()})
		return p.Items, p.NextAfter, err
	})
	if err != nil {
		return nil, err
	}
	attach(list, liveLeases, func(l leases.Lease) string { return l.Actor }, func(a *Actor, l leases.Lease) { a.Leases = append(a.Leases, l) })
	attach(list, openAsks, func(k asks.Ask) string { return k.Actor }, func(a *Actor, k asks.Ask) { a.Asks = append(a.Asks, k) })
	return list, nil
}

// attach adds each item to the actor whose handle it names; an item of an
// actor that is not in list is dropped.
func attach[T any](list []Actor, items []T, handle func(T) string, add func(*Actor, T)) {
	at := map[string]int{}
	for i := range list {
		at[list[i].Handle] = i
	}
	for _, it := range items {
		if i, ok := at[handle(it)]; ok {
			add(&list[i], it)
		}
	}
}

// pages reads every page fetch answers, following its next-after value.
func pages[T any](fetch func(after string) ([]T, string, error)) ([]T, error) {
	var all []T
	for after := ""; ; {
		items, next, err := fetch(after)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		after = next
	}
}

// actors reads the actor rows, newest seen first, with empty lease and ask lists.
func (s Store) actors(ctx context.Context, handle string) ([]Actor, error) {
	query, args := `SELECT handle, kind, way_back, last_seen_at FROM actors`, []any(nil)
	if handle != "" {
		query, args = query+` WHERE handle = ?`, []any{handle}
	}
	rows, err := s.Store.Reader().QueryContext(ctx, s.Store.Dialect().Rebind(query+` ORDER BY last_seen_at DESC, handle`), args...)
	if err != nil {
		return nil, fmt.Errorf("read actors: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Actor{}
	for rows.Next() {
		a := Actor{Leases: []leases.Lease{}, Asks: []asks.Ask{}}
		var wayBack sql.NullString
		var seen int64
		if err := rows.Scan(&a.Handle, &a.Kind, &wayBack, &seen); err != nil {
			return nil, fmt.Errorf("read actors: %w", err)
		}
		a.WayBack, a.LastSeen = wayBack.String, store.DecodeTime(seen)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read actors: %w", err)
	}
	return out, nil
}
