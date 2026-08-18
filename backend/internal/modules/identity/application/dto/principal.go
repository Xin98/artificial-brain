package dto

import "context"

// Principal is the authenticated subject every other context receives. It is
// resolved by Identity's session middleware and never re-derived from cookies
// downstream.
type Principal struct {
	UserID      string
	WorkspaceID string
	SessionID   string
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
