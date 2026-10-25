package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/jackc/pgx/v5/pgxpool"
)

// registryWriteMu makes the daemon's registry writers take turns. A save removes every server
// absent from the document it saves (applyManagedMCPConfig), so two writers that each loaded
// the registry before the other saved would erase each other's change. An MCP install verifies
// for up to 120 s and a plugin install for as long per connector: the window is real (spec
// 2026-10-25-pack-install §6b).
var registryWriteMu sync.Mutex

// updateManagedMCPConfig applies fn to the registry inside registryWriteMu, on a document
// loaded inside it, and saves the result with its audit row. A change fn refuses saves nothing.
// The `aura mcp` CLI is another process and does not take this lock.
func updateManagedMCPConfig(ctx context.Context, pool *pgxpool.Pool, audit mcpmanager.MCPAuditInsert, fn func(*mcp.ManagedConfig) error) error {
	registryWriteMu.Lock()
	defer registryWriteMu.Unlock()
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return fmt.Errorf("load managed MCP config: %w", err)
	}
	mcp.Normalize(&doc)
	if err := fn(&doc); err != nil {
		return err
	}
	return saveManagedMCPConfig(ctx, pool, doc, audit)
}
