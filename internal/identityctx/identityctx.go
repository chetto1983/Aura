package identityctx

import "context"

// LocalOperatorIdentity is the seeded `local` operator identity (migration 0004,
// UUID …0001). It is the fail-closed fallback owner for any request that carries no
// authenticated principal: a no-principal path is attributed to the local operator
// rather than left unscoped. Both :User-writing subsystems share this single value —
// document ingestion (documents.OperatorIdentity aliases it) and memory-MCP tool
// scoping — so they agree on who owns no-principal data.
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
