package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/readiness"
)

// memoryReadinessOwner is a synthetic identity that exists only for this probe.
// Under one-database-per-identity it resolves to its own database, so readiness
// neither reads nor writes anything a person owns.
const memoryReadinessOwner = "00000000-0000-0000-0000-0000000000ff"

func memoryReadinessProbe(chat *chatEnv) (agui.ReadinessProbe, bool) {
	required := false
	for _, server := range chat.cfg.MCPPolicies {
		if mcp.IsSharedAdminGoverned(server) {
			required = true
			break
		}
	}
	if !required {
		return agui.ReadinessProbe{}, false
	}
	return agui.ReadinessProbe{
		Name: "memory",
		Code: readiness.CodeMemoryUnavailable,
		Check: func(ctx context.Context) error {
			if chat.toolHandles.Memory == nil {
				return errors.New("required memory capability is not mounted")
			}
			return checkMemoryReadiness(ctx, chat.toolHandles.Memory)
		},
	}, true
}

func checkMemoryReadiness(ctx context.Context, client mcptools.HostClient) error {
	if client == nil {
		return errors.New("memory client is unavailable")
	}
	// The arguments are the mounted tool's. `memory_types` is gone with the
	// agent-memory MCP that declared it — the ArcadeDB tool rejects unknown
	// properties, so leaving it in failed every probe, answered /readyz with 503,
	// and took the cockpit down over a memory that worked.
	//
	// `user_identifier` stays, and now means more than it did: memory is one
	// DATABASE per identity, so the synthetic readiness owner gets its own, and a
	// health check cannot read or disturb a real person's memory. This probe
	// builds its map literally rather than through the bridge, so it has to stamp
	// the call itself.
	text, err := client.CallTool(ctx, "memory_search", map[string]any{
		"query":           "Aura readiness",
		"limit":           1,
		"user_identifier": memoryReadinessOwner,
	})
	if err != nil {
		return fmt.Errorf("memory functional search: %w", err)
	}
	// What readiness proves is that the tool ANSWERED in its own shape — a `facts`
	// array, empty or not. An empty memory is ready; an unparseable answer is not.
	var response struct {
		Facts []json.RawMessage `json:"facts"`
		Error string            `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("decode memory readiness result: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	if strings.TrimSpace(response.Error) != "" {
		return errors.New("memory functional search reported a domain failure")
	}
	if response.Facts == nil {
		return errors.New("memory functional search omitted the facts array")
	}
	return nil
}
