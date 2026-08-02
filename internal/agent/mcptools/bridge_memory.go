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

// defaultDeferred is now true for EVERY bridged server, memory included. Memory used to be
// the exception, on the theory that recall is too central to hide. Measured, that exception
// cost ~2.7k tokens of manifest on every single turn — six full schemas, carried to answer
// "ok" — which is 20% of the fixed prefix for a capability the model reaches through
// tool_search like any other. The 14 calendar and 14 whatsapp tools were already deferred
// and are no less central to their domains.
func (bridgePolicy) defaultDeferred() bool {
	return true
}

// memoryHiddenFromModel is a deliberately SMALL deny list, not a curated surface: the
// sidecar's own long-term tools reach the model, and only what Aura owns elsewhere or does
// not want written by hand stays out. Everything here is Deferred like the rest, so hiding
// buys nothing on the per-turn manifest — it costs only when tool_search would have
// returned it. Hiding is therefore a claim that the model is BETTER OFF unable to call it.
//
// memory_add_entity and memory_create_relationship used to be on this list and are not
// anymore. They were the only two tools that create a node and an edge, and without them
// the long-term graph could not be built at all: measured on the live deployment
// 2026-07-30, 22 of 26 facts had no ABOUT_SUBJECT edge, and an agent asked to register a
// client wrote three dangling facts, reported them as "collegati al nodo nel grafo", and
// was wrong — the fact writer deliberately does not mint entities
// (_link_fact_to_subject_entity), so with no add_entity nothing ever creates one. Hiding
// them did not save a manifest; it removed the graph from the graph memory.
// memoryHiddenFromModel used to list six tools — store_message, get_conversation,
// list_sessions, get_context, get_facts, store_profile — none of which the ArcadeDB memory
// server implements. A deny list denying tools that cannot be called is a deny list that
// denies nothing, and it read like protection while providing none.
//
// What actually needs hiding is maintenance: re-embedding is an operator decision about
// infrastructure — it rewrites every vector in the corpus and its cost scales with the
// corpus, not with the turn. An agent reaching for it mid-conversation is never right.
var memoryHiddenFromModel = map[string]struct{}{
	"memory_reembed": {},
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
