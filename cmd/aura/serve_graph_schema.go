// serve_graph_schema.go builds the cockpit's tenant-scoped graph view over ArcadeDB.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

// arcadeTenantGraph reads ONE identity's catalogue and graph through that
// identity's own derived credential: the same database name and password the
// memory sidecar provisions with (internal/arcadedb/tenant.go). The server
// refuses a credential scoped elsewhere.
//
// It caches nothing. arcadedb.New performs no I/O and its http.Client uses the
// shared default transport, so requests reuse pooled connections without a
// credential-bearing client cache that would need de-provisioning rules.
type arcadeTenantGraph struct {
	base        arcadedb.Config
	credentials *arcadedb.TenantCredentials
}

func (a arcadeTenantGraph) client(identityID string) (*arcadedb.Client, error) {
	database, err := arcadedb.DatabaseFor(identityID)
	if err != nil {
		return nil, fmt.Errorf("memory graph: %w", err)
	}
	cfg := a.base
	cfg.Database = database
	cfg.User = arcadedb.TenantUserFor(database)
	cfg.Password = a.credentials.PasswordFor(database)
	client, err := arcadedb.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("memory graph: %w", err)
	}
	return client, nil
}

func (a arcadeTenantGraph) Schema(ctx context.Context, identityID string) (arcadedb.Schema, error) {
	client, err := a.client(identityID)
	if err != nil {
		return arcadedb.Schema{}, err
	}
	return client.Schema(ctx)
}

func (a arcadeTenantGraph) QueryGraph(
	ctx context.Context,
	identityID string,
	statement string,
	params map[string]any,
	limit int,
) (arcadedb.StudioGraph, error) {
	client, err := a.client(identityID)
	if err != nil {
		return arcadedb.StudioGraph{}, err
	}
	return client.QueryStudioGraph(ctx, statement, params, limit)
}

// buildArcadeGraphView wires the graph view, or returns nil when the memory
// server or tenant derivation secret is not configured. Nil keeps both routes at
// their existing best-effort 503 contract instead of refusing to boot Aura.
func buildArcadeGraphView(cfg *config.Config) agui.GraphView {
	if cfg == nil || strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		slog.Warn("aura serve: no ArcadeDB server configured — graph explorer unavailable")
		return nil
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		slog.Warn("aura serve: no ArcadeDB tenant secret — graph explorer unavailable", "error", err)
		return nil
	}
	// Database is deliberately unset: the authenticated identity selects it on
	// every request. A default here would create an unsafe fallback.
	base := arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}
	return agui.NewArcadeGraphView(arcadeTenantGraph{base: base, credentials: credentials})
}
