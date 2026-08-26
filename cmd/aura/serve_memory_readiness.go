package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/readiness"
)

// memoryReadinessOwner is a synthetic identity that exists only for this probe.
// Under one-database-per-identity it resolves to its own database, so readiness
// neither reads nor writes anything a person owns.
const memoryReadinessOwner = "00000000-0000-0000-0000-0000000000ff"

func memoryReadinessProbe(chat *chatEnv) (agui.ReadinessProbe, bool) {
	_, policies, err := mcpRuntimeSet()
	if err != nil {
		return agui.ReadinessProbe{}, false
	}
	memoryServer := ""
	for name, server := range policies {
		if mcp.IsSharedAdminGoverned(server) {
			memoryServer = name
			break
		}
	}
	if memoryServer == "" {
		return agui.ReadinessProbe{}, false
	}
	return agui.ReadinessProbe{
		Name: "memory",
		Code: readiness.CodeMemoryUnavailable,
		Check: func(ctx context.Context) error {
			client := chat.toolHandles.Memory
			owner := memoryReadinessOwner
			if chat.liveMCP != nil {
				if live, liveOwner := chat.liveMCP.OwnedHost(memoryServer); live != nil {
					client = live
					if strings.TrimSpace(liveOwner) != "" {
						owner = liveOwner
					}
				}
			}
			if client == nil {
				return errors.New("required memory capability is not mounted")
			}
			return checkMemoryReadiness(ctx, client, owner)
		},
	}, true
}

func checkMemoryReadiness(ctx context.Context, client *mcptools.MountedServer, owner string) error {
	if client == nil {
		return errors.New("memory client is unavailable")
	}
	// The arguments are the mounted tool's. `memory_types` is gone with the
	// agent-memory MCP that declared it — the ArcadeDB tool rejects unknown
	// properties, so leaving it in failed every probe, answered /readyz with 503,
	// and took the cockpit down over a memory that worked.
	//
	// A static mount uses the isolated synthetic owner. An OAuth mount reuses the
	// subject whose stored grant opened the live session: the functional read remains
	// tenant-scoped and never creates or borrows a session from another identity.
	text, err := client.CallToolText(identityctx.WithIdentityID(ctx, owner), "memory_search", map[string]any{
		"query": "Aura readiness",
		"limit": 1,
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
	if err := ensureMemoryJSONEOF(decoder); err != nil {
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

// ensureMemoryJSONEOF rejects a tool result that carries a second JSON value after the
// one we decoded. A sidecar that concatenates two objects would otherwise pass the probe
// on the first and hide whatever the second said. It lived next to the dynamic-recall
// decoder until that leg was deleted; the readiness probe is its only caller now.
func ensureMemoryJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode memory readiness result: trailing JSON value")
		}
		return fmt.Errorf("decode memory readiness result trailing data: %w", err)
	}
	return nil
}
