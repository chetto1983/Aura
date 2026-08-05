package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/arcadedb"
)

type tenantClientResolver interface {
	For(ctx context.Context, identityID string) (*arcadedb.Client, error)
}

// tenants preserves the MCP handlers' narrow dependency while delegating all
// credential, provisioning, schema, cache, and concurrency behavior to the
// shared resolver used by the document pipeline.
type tenants struct {
	resolver tenantClientResolver
}

func newTenants(
	base arcadedb.Config,
	admin *arcadedb.Client,
	embedder arcadedb.Embedder,
	credentials *arcadedb.TenantCredentials,
) *tenants {
	return &tenants{resolver: arcadedb.NewTenantClients(base, admin, embedder, credentials)}
}

func (t *tenants) For(ctx context.Context, identityID string) (*arcadedb.Client, error) {
	if t == nil || t.resolver == nil {
		return nil, fmt.Errorf("memory tenant resolver is not configured")
	}
	return t.resolver.For(ctx, identityID)
}
