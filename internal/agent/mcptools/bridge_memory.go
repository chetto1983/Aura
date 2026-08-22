package mcptools

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

type bridgePolicy struct {
	memory       bool
	recipeSource string
	// views is the MCP Apps gate: may this mount put its own HTML in front of the
	// operator (bridge_views.go)? It is set from the mount's TRUST CLASS, so a
	// policy built without one — defaultBridgePolicy, every test double — renders
	// nothing. That default is deliberate and fail-closed: a plain `mcpServers`
	// entry declares no trust class at all, and "no class" must not read as "the
	// most trusted one".
	views bool
	// alwaysLoaded is frozen once, at mount, by bridgeToolsWithPolicy's call to
	// grantLoadedSlot (bridge_deferral.go, D-27 / TOOL-14 amendment #123): whether
	// this mount's model-facing tool count won one of the two global
	// always-loaded slots. Every bridgedTool built from one mount carries the
	// identical value and never recomputes it — that freeze is what lets
	// refreshSpec survive a reconnect without flipping a tool's manifest
	// presence out from under the model's KV-cache prefix.
	alwaysLoaded bool
	// modelFacingCount is the model-facing tool count grantLoadedSlot was scored
	// against at mount, frozen alongside alwaysLoaded so a reconnect can report
	// what changed (warnIfDeferralWouldFlip) without ever recomputing the
	// decision itself.
	modelFacingCount int
}

func defaultBridgePolicy(namespace string) bridgePolicy {
	return bridgePolicy{memory: namespace == "memory"}
}

// defaultDeferred is D-27's count arithmetic (TOOL-14 / PRD amendment #123): a
// mount exposing <= maxAlwaysLoadedMCPTools model-facing tools, while the
// global maxAlwaysLoadedMCPSlots budget has room, earns an always-loaded slot
// and is therefore NOT deferred. alwaysLoaded is scored once at mount by
// bridgeToolsWithPolicy (bridge.go) and frozen on every bridgedTool it builds;
// the zero value (false) is every existing caller's unconditional "deferred",
// so a bridgePolicy built without going through that arithmetic —
// defaultBridgePolicy before Bridge scores it, a bare literal, a test double —
// keeps today's behaviour. tool_search keeps a deferred tool discoverable
// without carrying its full schema in every model request.
func (p bridgePolicy) defaultDeferred() bool {
	return !p.alwaysLoaded
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

// IdentityMetaMiddleware stamps _meta.aura.user_identifier on every tools/call
// request a memory-policy mount sends, replacing withMemoryUserIdentifier's
// argument injection (D-108: identity leaves Aura only in _meta now). It lives
// here, not in internal/mcp, because the memory policy lives here and
// internal/mcp has no identity concern (mirrors OperationMetaMiddleware's shape
// exactly: short-circuit on method != "tools/call", type-assert
// *sdkmcp.CallToolParams, pass through on failure).
//
// The ctx-absent default (identityctx.LocalOperatorIdentity) is
// withMemoryUserIdentifier's preserved behaviour, NOT the dual-source fallback
// D-108 forbids: it is a single source (ctx, defaulted) writing to a single
// destination (_meta). D-108's prohibition is specifically an ARGUMENT fallback
// alongside a _meta value on the SAME call — that path does not exist here.
func IdentityMetaMiddleware() sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			params, ok := req.GetParams().(*sdkmcp.CallToolParams)
			if !ok {
				return next(ctx, method, req)
			}
			identityID := identityctx.IdentityID(ctx)
			if identityID == "" {
				identityID = identityctx.LocalOperatorIdentity
			}
			mcp.SetAuraMetaField(params, mcp.MetaFieldUserIdentifier, identityID)
			return next(ctx, method, req)
		}
	}
}
