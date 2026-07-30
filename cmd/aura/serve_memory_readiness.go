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

const memoryReadinessOwner = "00000000-0000-0000-0000-000000000001"

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
	text, err := client.CallTool(ctx, "memory_search", map[string]any{
		"query":           "Aura readiness",
		"memory_types":    []string{"preferences"},
		"limit":           1,
		"user_identifier": memoryReadinessOwner,
	})
	if err != nil {
		return fmt.Errorf("memory functional search: %w", err)
	}
	var response struct {
		Results map[string]json.RawMessage `json:"results"`
		Error   string                     `json:"error"`
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
	preferences, ok := response.Results["preferences"]
	if !ok {
		return errors.New("memory functional search omitted preferences results")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(preferences, &items); err != nil {
		return errors.New("memory functional search returned malformed preferences")
	}
	return nil
}
