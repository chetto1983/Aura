package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// Default-on catalog keys. Memory is a core capability everywhere; calculator is
// default-on only inside the Aura appliance image, where uvx is warm-cached.
const (
	memoryRecipeName     = "memory"
	calculatorRecipeName = "calculator"
)

// loadMCPServers composes the runtime MCP server set from the managed config doc, the
// AURA_MCP_SERVERS_JSON env override, and the default-on memory recipe (D-08). It returns the
// runnable servers + the policy map (managed recipes the operator may enable/disable).
func loadMCPServers() (map[string]mcp.ServerConfig, map[string]mcp.ManagedServer, error) {
	path, err := mcp.ManagedConfigPath()
	if err != nil {
		return nil, nil, err
	}
	managed, err := mcp.LoadManagedConfig(path)
	if err != nil {
		return nil, nil, err
	}
	managedServers, err := mcpmanager.RuntimeServers(managed)
	if err != nil {
		return nil, nil, err
	}
	runnableManaged, err := mcpmanager.RunnableManagedServers(managed)
	if err != nil {
		return nil, nil, err
	}
	policies := make(map[string]mcp.ManagedServer, len(runnableManaged))
	for name, server := range runnableManaged {
		policies[name] = server
	}
	envServers, err := parseMCPServersJSON(os.Getenv("AURA_MCP_SERVERS_JSON"))
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]mcp.ServerConfig, len(managedServers)+len(envServers))
	for name, cfg := range managedServers {
		out[name] = cfg
	}
	for name, cfg := range envServers {
		out[name] = cfg
		delete(policies, name)
	}
	// Default-on (D-08): memory is a core capability, so it mounts out of the box
	// with no `aura mcp install`. Injected AFTER the env delete loop so an
	// AURA_MCP_SERVERS_JSON override of `memory` still wins; respects an explicit
	// `aura mcp disable memory` (D-09). On a fresh machine this makes len(policies)
	// non-zero — the intended default-on behavior.
	injectDefaultOnMemory(policies, managed, envServers)
	injectDefaultOnContainerCalculator(policies, managed, envServers)
	if len(out) == 0 && len(policies) == 0 {
		return nil, nil, nil
	}
	return out, policies, nil
}

func parseMCPServersJSON(raw string) (map[string]mcp.ServerConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var wrapped struct {
		MCPServers map[string]mcp.ServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err != nil {
		return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: %w", err)
	}
	if wrapped.MCPServers != nil {
		return validateMCPServers(wrapped.MCPServers)
	}
	var direct map[string]mcp.ServerConfig
	if err := json.Unmarshal([]byte(raw), &direct); err != nil {
		return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: %w", err)
	}
	return validateMCPServers(direct)
}

func validateMCPServers(in map[string]mcp.ServerConfig) (map[string]mcp.ServerConfig, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]mcp.ServerConfig, len(in))
	for name, cfg := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: server name cannot be empty")
		}
		if strings.TrimSpace(cfg.Command) == "" {
			return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: server %q command cannot be empty", name)
		}
		cfg.Command = strings.TrimSpace(cfg.Command)
		out[name] = cfg
	}
	return out, nil
}

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

// injectDefaultOnContainerCalculator adds the calculator recipe in Aura's
// appliance container. The Docker image warm-caches the pinned uvx package, so
// the stdio server starts without relying on egress at first use. Local dev
// remains install-on-demand to avoid surprising non-container hosts without uvx.
func injectDefaultOnContainerCalculator(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig, envOverridden map[string]mcp.ServerConfig) {
	if os.Getenv("AURA_IN_CONTAINER") != "1" {
		return
	}
	if _, ok := policies[calculatorRecipeName]; ok {
		return
	}
	if _, ok := envOverridden[calculatorRecipeName]; ok {
		return
	}
	if _, ok := managed.MCPServers[calculatorRecipeName]; ok {
		return
	}
	recipe, ok := mcpmanager.LookupCatalog(calculatorRecipeName)
	if !ok {
		return
	}
	policies[calculatorRecipeName] = recipe.Server
}
