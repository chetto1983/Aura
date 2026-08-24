package main

import (
	"context"
	"maps"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mcp_registry_fake_test.go swaps the Postgres-backed registry for an in-memory one, so a
// CLI test can exercise argument parsing, validation and the mutation verbs without a
// database. It is the test-side implementation of the same seam LibreChat draws between
// ServerConfigsDB and ServerConfigsCache.

// withMemoryMCPRegistry points loadManagedMCPConfig/saveManagedMCPConfig at a document held
// in memory for the duration of the test, restoring the real registry afterwards.
func withMemoryMCPRegistry(t *testing.T) {
	t.Helper()
	doc := mcp.ManagedConfig{
		Version:    mcp.ManagedConfigVersion,
		MCPServers: map[string]mcp.ManagedServer{},
		Profiles:   map[string]mcp.ManagedProfile{},
	}
	loadPrev, savePrev := loadManagedMCPConfig, saveManagedMCPConfig
	t.Cleanup(func() { loadManagedMCPConfig, saveManagedMCPConfig = loadPrev, savePrev })
	loadManagedMCPConfig = func() (mcp.ManagedConfig, error) { return cloneManagedConfig(doc), nil }
	saveManagedMCPConfig = func(ctx context.Context, pool *pgxpool.Pool, next mcp.ManagedConfig, in mcpmanager.MCPAuditInsert) error {
		doc = cloneManagedConfig(next)
		if pool == nil {
			return nil
		}
		// The registry is faked; the ledger is not. A mutation dispatched with a live pool
		// still owes its audit row, and a test given a real pool is asking about exactly
		// that — so this half stays real, in the same transaction shape the production
		// function uses.
		return db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
			return mcpmanager.InsertMCPAuditTx(ctx, q, in)
		})
	}
}

// seedMCPRegistry puts doc in the in-memory registry as if an operator had installed it.
func seedMCPRegistry(t *testing.T, doc mcp.ManagedConfig) {
	t.Helper()
	if err := saveManagedMCPConfig(context.Background(), nil, doc, mcpmanager.MCPAuditInsert{}); err != nil {
		t.Fatalf("seed MCP registry: %v", err)
	}
}

// withDefaultOnRecipesOff declares memory, calendar and whatsapp explicitly disabled, so
// default-on injection leaves them alone.
//
// A test that counts mounted servers needs this. Default-on adds memory (and, in a
// container, calendar and whatsapp) to any document that stays silent about them, and the
// memory recipe points at a real port — so "1/1 HTTP MCP servers reachable" quietly became
// 2/2 on a machine that happened to be running the Aura stack. A test whose answer depends
// on what else is listening on the box is not a test.
func withDefaultOnRecipesOff(doc mcp.ManagedConfig) mcp.ManagedConfig {
	next := cloneManagedConfig(doc)
	off := false
	for _, name := range []string{"memory", "calendar", "whatsapp"} {
		if _, declared := next.MCPServers[name]; declared {
			continue
		}
		recipe, ok := mcpmanager.LookupCatalog(name)
		if !ok {
			continue
		}
		pinned := recipe.Server
		pinned.Enabled = &off
		next.MCPServers[name] = pinned
	}
	return next
}

// readMCPRegistry returns what the registry holds now.
func readMCPRegistry(t *testing.T) mcp.ManagedConfig {
	t.Helper()
	doc, err := loadManagedMCPConfig()
	if err != nil {
		t.Fatalf("read MCP registry: %v", err)
	}
	return doc
}

// withRuntimeMCPServers pins what doctorRuntimeMCPServers resolves, so the probe's
// aggregation can be checked without a registry at all.
func withRuntimeMCPServers(t *testing.T, servers map[string]mcp.ManagedServer) {
	t.Helper()
	prev := doctorRuntimeMCPServers
	doctorRuntimeMCPServers = func() (map[string]mcp.ManagedServer, error) { return servers, nil }
	t.Cleanup(func() { doctorRuntimeMCPServers = prev })
}

// cloneManagedConfig deep-copies the maps so a caller mutating what it loaded cannot reach
// back into the stored document — the real registry hands out fresh rows every time, and a
// fake that shared them would hide exactly the aliasing bugs it is meant to catch.
func cloneManagedConfig(doc mcp.ManagedConfig) mcp.ManagedConfig {
	out := doc
	out.MCPServers = make(map[string]mcp.ManagedServer, len(doc.MCPServers))
	maps.Copy(out.MCPServers, doc.MCPServers)
	out.Profiles = make(map[string]mcp.ManagedProfile, len(doc.Profiles))
	maps.Copy(out.Profiles, doc.Profiles)
	return out
}
