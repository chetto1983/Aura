package identityctx

import "context"

type key struct{}

// WithIdentityID returns a context carrying the authenticated or conversation-owned
// Aura identity id. An empty id leaves the context unchanged.
func WithIdentityID(ctx context.Context, identityID string) context.Context {
	if identityID == "" {
		return ctx
	}
	return context.WithValue(ctx, key{}, identityID)
}

// IdentityID returns the Aura identity id carried on ctx, or "" when the caller is
// not scoped to a user.
func IdentityID(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}
