package config

import (
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// memoryRecipeName is the catalog/policy key for the default-on agent-memory recipe.
const memoryRecipeName = "memory"

// injectDefaultOnMemory adds the `memory` recipe to policies unless the operator
// opted out (D-08 default-on, D-09 respect disable). Memory is a core capability →
// on out of the box, so a fresh machine mounts it with no `aura mcp install`.
//
// Precedence (highest first):
//  1. An explicit/operator entry already in policies wins (do not override a
//     customized URL).
//  2. An AURA_MCP_SERVERS_JSON override (already landed in envOverridden, with the
//     overlapping policy deleted by the caller) wins — do not re-inject.
//  3. An explicit `aura mcp disable memory` (Enabled=false in the managed doc) keeps
//     memory unmounted, mirroring RunnableManagedServers' disable check.
//  4. Otherwise inject LookupCatalog("memory").Server.
func injectDefaultOnMemory(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig, envOverridden map[string]mcp.ServerConfig) {
	if _, ok := policies[memoryRecipeName]; ok {
		return
	}
	if _, ok := envOverridden[memoryRecipeName]; ok {
		return
	}
	if s, ok := managed.MCPServers[memoryRecipeName]; ok && s.Enabled != nil && !*s.Enabled {
		return
	}
	recipe, ok := mcpmanager.LookupCatalog(memoryRecipeName)
	if !ok {
		return
	}
	policies[memoryRecipeName] = recipe.Server
}
