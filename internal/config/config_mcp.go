package config

import (
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// memoryRecipeName is the catalog/policy key for the default-on agent-memory recipe.
const memoryRecipeName = "memory"

// injectDefaultOnMemory adds the `memory` recipe to policies unless the operator
// has ANY say of their own (D-08 default-on, D-09 respect disable). Memory is a
// core capability → on out of the box, so a fresh machine mounts it with no
// `aura mcp install`.
//
// Precedence (highest first):
//  1. An AURA_MCP_SERVERS_JSON override (already landed in envOverridden, with the
//     overlapping policy deleted by the caller) wins — do not re-inject.
//  2. ANY explicit `memory` entry in the managed doc wins — enabled (already in
//     policies), disabled (`aura mcp disable memory`), trust-blocked, or excluded
//     by the active profile. The check is against the UNFILTERED
//     managed.MCPServers, not the profile-filtered policies map: a customized URL
//     excluded by the active profile must NOT be silently replaced by the catalog
//     recipe, and a profile that excludes memory keeps it unmounted (CR-01).
//  3. Otherwise (no operator entry at all) inject LookupCatalog("memory").Server.
func injectDefaultOnMemory(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig, envOverridden map[string]mcp.ServerConfig) {
	if _, ok := policies[memoryRecipeName]; ok {
		return
	}
	if _, ok := envOverridden[memoryRecipeName]; ok {
		return
	}
	if _, ok := managed.MCPServers[memoryRecipeName]; ok {
		// Explicit operator entry (disabled, blocked, or profile-excluded —
		// the enabled+active case already returned via policies above).
		return
	}
	recipe, ok := mcpmanager.LookupCatalog(memoryRecipeName)
	if !ok {
		return
	}
	policies[memoryRecipeName] = recipe.Server
}
