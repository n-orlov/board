package initiatives

import (
	"context"
	"errors"

	"github.com/n-orlov/board/internal/audit"
)

// The built-in inbox initiative serve creates on start (SPEC 4.2).
const (
	InboxSlug        = "inbox"
	InboxTitle       = "Inbox"
	InboxDescription = "Work that fits no other initiative yet."
	// BootstrapPrincipal is the principal of the event that records its creation.
	BootstrapPrincipal = "system:bootstrap"
)

// EnsureInbox creates the inbox initiative, made by the operator actor
// (operator is its handle, created when absent) under BootstrapPrincipal, and
// reports whether this call created it. An inbox that exists is left as it is,
// whatever its title, tags and status, and so is the case of a second server
// that creates it a moment earlier: the slug is unique, so the loser's
// transaction leaves nothing behind.
func (s Store) EnsureInbox(ctx context.Context, operator string) (bool, error) {
	_, err := s.Get(ctx, InboxSlug)
	if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	_, err = s.Create(ctx, audit.Actor{Handle: operator, Operator: true}, BootstrapPrincipal,
		New{Slug: InboxSlug, Title: InboxTitle, Description: InboxDescription})
	if errors.Is(err, ErrExists) {
		return false, nil
	}
	return err == nil, err
}
