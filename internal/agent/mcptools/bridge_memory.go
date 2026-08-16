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
