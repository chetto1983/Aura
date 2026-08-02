// serve_graph_schema.go builds the cockpit's graph view over ArcadeDB.
//
// It replaced a boot-time mcp-neo4j-cypher subprocess whose GraphView could draw the
// canvas. ArcadeDB's cannot: see internal/agui/graph_arcadedb.go for what was lost and
// why it was not ported. What is wired here is the read that survives — one identity's
// type catalogue — and it needs no subprocess, no boot-time handle and nothing to close.
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

// arcadeTenantSchema reads ONE identity's type catalogue through that identity's own
// derived credential — the same database name and the same derived password the memory
// sidecar provisions with (internal/arcadedb/tenant.go). The server refuses a credential
// scoped elsewhere, so the cockpit cannot read another person's memory shape even if this
// code were handed the wrong identity.
//
// It caches nothing. arcadedb.New performs no I/O and the http.Client it builds uses the
// shared default transport, so a per-request client costs an allocation and reuses the
// pooled connection — cheaper than the invalidation rules a cache keyed by identity would
// need once a database is dropped by de-provisioning.
type arcadeTenantSchema struct {
	base        arcadedb.Config
	credentials *arcadedb.TenantCredentials
}

func (a arcadeTenantSchema) Schema(ctx context.Context, identityID string) (arcadedb.Schema, error) {
	database, err := arcadedb.DatabaseFor(identityID)
	if err != nil {
		return arcadedb.Schema{}, fmt.Errorf("memory schema: %w", err)
	}
	cfg := a.base
	cfg.Database = database
	cfg.User = arcadedb.TenantUserFor(database)
	cfg.Password = a.credentials.PasswordFor(database)
	client, err := arcadedb.New(cfg)
	if err != nil {
		return arcadedb.Schema{}, fmt.Errorf("memory schema: %w", err)
	}
	return client.Schema(ctx)
}

// buildArcadeGraphView wires the graph view, or returns nil when the memory server or the
// tenant derivation secret is not configured. Nil leaves both /api/graph/* routes at 503,
// which is the same best-effort contract the previous graph client had: a cockpit panel
// that cannot answer is not a reason to refuse to boot.
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
	// Database is left unset here on purpose: it is the per-identity one, chosen per
	// request. A default sitting in the base config is a default something could use.
	base := arcadedb.Config{BaseURL: cfg.ArcadeDB.BaseURL}
	return agui.NewArcadeGraphView(arcadeTenantSchema{base: base, credentials: credentials})
}
