package mcptools

import (
	"context"
	"encoding/json"

	"github.com/chetto1983/aura/internal/identityctx"
)

type bridgePolicy struct {
	memory       bool
	recipeSource string
}

func defaultBridgePolicy(namespace string) bridgePolicy {
	return bridgePolicy{memory: namespace == "memory"}
}

// Every bridged MCP tool is deferred. tool_search keeps memory discoverable
// without carrying its full schemas in every model request.
func (bridgePolicy) defaultDeferred() bool {
	return true
}

// The model gets one read plan, memory_recall. Path-specific reads remain active
// host primitives for automatic context, the CLI, readiness, and graph inspection;
// exposing them through tool_search made the model plan Aura's retrieval internals.
var memoryHiddenFromModel = map[string]struct{}{
	"graph_schema":       {},
	"memory_digest":      {},
	"memory_entities":    {},
	"memory_facts_about": {},
	"memory_reembed":     {},
	"memory_search":      {},
}

func (p bridgePolicy) modelFacing(tool string) bool {
	if !p.memory {
		return true
	}
	_, hidden := memoryHiddenFromModel[tool]
	return !hidden
}

func (b *bridgedTool) withMemoryUserIdentifier(ctx context.Context, args map[string]any) map[string]any {
	if !b.policy.memory || !acceptsUserIdentifier(b.Spec().Parameters) {
		return args
	}
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		identityID = identityctx.LocalOperatorIdentity
	}
	if args == nil {
		args = make(map[string]any, 1)
	}
	args["user_identifier"] = identityID
	return args
}

func acceptsUserIdentifier(parameters json.RawMessage) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(parameters, &schema); err != nil || schema.Properties == nil {
		return true
	}
	_, ok := schema.Properties["user_identifier"]
	return ok
}
