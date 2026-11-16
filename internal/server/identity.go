package server

import (
	"context"

	"github.com/n-orlov/village-hall/internal/auth"
)

type identityContextKey struct{}

// withIdentity records who made the request: the principal that authenticated
// and the actor it acts as (SPEC 3.1).
func withIdentity(ctx context.Context, who auth.Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, who)
}

func identityFrom(ctx context.Context) (auth.Identity, bool) {
	who, ok := ctx.Value(identityContextKey{}).(auth.Identity)
	return who, ok
}
