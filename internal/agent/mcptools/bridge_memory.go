package mcptools

import (
	"context"
	"encoding/json"

	"github.com/chetto1983/aura/internal/identityctx"
)

type bridgePolicy struct {
	memory bool
}

func defaultBridgePolicy(namespace string) bridgePolicy {
	return bridgePolicy{memory: namespace == "memory"}
}

func (p bridgePolicy) defaultDeferred() bool {
	return !p.memory
}

var memoryHiddenFromModel = map[string]struct{}{
	"memory_store_message":       {},
	"memory_get_context":         {},
	"memory_get_conversation":    {},
	"memory_list_sessions":       {},
	"memory_add_entity":          {},
	"memory_store_profile":       {},
	"memory_create_relationship": {},
	"memory_get_facts":           {},
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
