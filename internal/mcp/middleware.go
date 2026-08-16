package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/idempotency"
)

// middleware.go carries the _meta.aura namespace contract. tool_methods.go:27-40
// (deleted in plan 45.1-03) built this stamp by hand into a bare params map on
// every call; OperationMetaMiddleware re-expresses the SAME stamp against the
// SDK's own Client.AddSendingMiddleware seam (mcp/client.go:1129).
const (
	// MetaNamespaceAura is the _meta key under which every Aura-authored field
	// lives, so Aura's own additions never collide with the SDK's own
	// io.modelcontextprotocol/* triple stamped by injectRequestMeta.
	MetaNamespaceAura = "aura"

	MetaFieldOperationKey         = "operation_key"
	MetaFieldOperationScope       = "operation_scope"
	MetaFieldOperationFingerprint = "operation_fingerprint"

	// MetaFieldUserIdentifier is the _meta.aura field the calling identity travels
	// in (plan 45.1-05/D-108). It replaces the "user_identifier" tool ARGUMENT: the
	// model never declares or sees this field, and cmd/arcadedb-mcp reads it from
	// nowhere else. Both the agent bridge (bridge_memory.go's IdentityMetaMiddleware)
	// and the CLI (cmd/aura/mcp_tools.go's callSessionText) stamp it through
	// SetAuraMetaField, so a key-name divergence between the two writers and the
	// server's own reader is a build-time-testable invariant, not a hope.
	MetaFieldUserIdentifier = "user_identifier"
)

// auraMeta fetches (allocating if absent) the "aura" namespace map nested inside
// params' _meta, sets it back, and returns it — so a later plan (45.1-05, the
// identity field) can reuse this exact merge dance instead of duplicating it.
//
// The SDK's own injectRequestMeta leaves keys already present in _meta untouched
// (go-sdk v1.7.0 mcp/client.go:516-527: "Keys already present in params.Meta are
// not overwritten"), so the "aura" namespace this builds and the
// io.modelcontextprotocol/protocolVersion / clientInfo / clientCapabilities triple
// the SDK stamps on every new-protocol request coexist without contention. This is
// non-obvious enough that CLAUDE.md's documentation-first rule wants it cited here
// rather than rediscovered by trial.
func auraMeta(params *sdkmcp.CallToolParams) map[string]any {
	m := params.GetMeta()
	if m == nil {
		m = map[string]any{}
	}
	aura, _ := m[MetaNamespaceAura].(map[string]any)
	if aura == nil {
		aura = map[string]any{}
	}
	m[MetaNamespaceAura] = aura
	params.SetMeta(m)
	return aura
}

// SetAuraMetaField sets one field under _meta.aura on params, built on auraMeta's
// fetch-or-allocate merge dance so a second writer (the identity middleware, the
// CLI's callSessionText) cannot drift from OperationMetaMiddleware's own stamp —
// one writer, two call sites, per this plan's <key_links> contract.
func SetAuraMetaField(params *sdkmcp.CallToolParams, field string, value any) {
	aura := auraMeta(params)
	aura[field] = value
}

// OperationMetaMiddleware stamps _meta.aura.{operation_key,operation_scope,
// operation_fingerprint} on every tools/call request that carries an idempotency
// operation on its context, preserving tool_methods.go:32-40's stamp verbatim.
// RESEARCH Pitfall 4 found no current reader of these keys off the wire, but
// absence-of-evidence is not evidence-of-absence — the stamp stays regardless.
//
// A request whose method isn't "tools/call", or whose params aren't
// *sdkmcp.CallToolParams, passes through unchanged. A request with no operation on
// its context leaves params.Meta untouched (nil stays nil): the middleware never
// allocates _meta speculatively.
func OperationMetaMiddleware() sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			params, ok := req.GetParams().(*sdkmcp.CallToolParams)
			if !ok {
				return next(ctx, method, req)
			}
			if operation, ok := idempotency.OperationFromContext(ctx); ok {
				aura := auraMeta(params)
				aura[MetaFieldOperationKey] = operation.Key.Key
				aura[MetaFieldOperationScope] = string(operation.Key.Scope)
				aura[MetaFieldOperationFingerprint] = idempotency.FingerprintHex(operation.Fingerprint)
			}
			return next(ctx, method, req)
		}
	}
}
