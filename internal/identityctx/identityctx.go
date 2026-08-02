package identityctx

import "context"

// LocalOperatorIdentity is the seeded `local` operator identity (migration 0004,
// UUID …0001). It is the owner of no-principal data ONLY until an operator enrolls:
// serve_auth.go's retireLegacyLocalIdentityForAuthulaUser migrates every Postgres
// reference onto the enrolled identity and then DELETES this row.
//
// So it is a seed, not a fallback. Using it as one after first login attributes data to
// a tenant that no longer exists, and — because the retirement rewrites Postgres rows
// only — silently forks memory in two: memory is one ArcadeDB database per identity, so
// the cockpit reads the enrolled identity's database while a caller falling back to this
// constant writes into the retired one, and neither sees the other. Resolve a
// no-principal owner with OperatorIdentity instead, which returns this value only while
// it is still the truth.
const LocalOperatorIdentity = "00000000-0000-0000-0000-000000000001"

// CLIServiceIdentity is the non-human principal used only to own durable CLI
// idempotency records. Unlike LocalOperatorIdentity, it survives first-login
// retirement of the legacy `local` identity (serve_auth.go) and carries no user
// capabilities. Migration 0049 seeds it as kind=service.
const CLIServiceIdentity = "00000000-0000-0000-0000-000000000039"

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
