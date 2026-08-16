package main

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// identity.go is the server-side half of D-108's hard cut: the calling identity
// reaches this binary in stateless _meta.aura.user_identifier and from nowhere
// else. auraMetaKey/metaFieldUserIdentifier MUST match internal/mcp's
// MetaNamespaceAura/MetaFieldUserIdentifier BY VALUE — this binary is a separate
// `main` package and cannot import internal/mcp for a shared constant without
// pulling agent-side policy into the memory sidecar, so the two pairs are pinned
// equal by identity_test.go's TestMetaKeyConstantsMatchClient instead. A silent
// divergence here refuses every call (fail-closed, not a tenant leak) but still
// takes memory down, which is why it is worth catching at build/test time rather
// than discovering it live.
const (
	auraMetaKey             = "aura"
	metaFieldUserIdentifier = "user_identifier"
)

// errMissingIdentityMeta is returned — as a plain Go error, never a *jsonrpc.Error
// — by every memory tool handler when the caller's _meta carries no usable
// identity. See identityFromMeta's doc comment for why a plain error is the
// correct (and the ONLY correct) shape here.
var errMissingIdentityMeta = errors.New("missing required identity _meta: aura.user_identifier is absent or empty; the caller must stamp the calling identity — each identity has its own database and cannot reach another's")

// identityFromMeta is the fail-closed read every memory tool handler starts
// with: it resolves the calling identity from req's stateless _meta and REFUSES
// (returns "", errMissingIdentityMeta) on any of absent, empty, whitespace-only,
// or non-string. Nil-request-safe and nil-Params-safe so a delegate handler that
// is (or ever becomes) reachable with req==nil refuses cleanly instead of
// panicking — see tool_memory.go's memoryRecallHandler, whose two delegate calls
// are exactly that shape.
//
// Return a plain Go error, not a hand-built *mcp.CallToolResult, and not a
// *jsonrpc.Error. Measured against the pinned SDK (go-sdk@v1.7.0
// mcp/server.go:383-392, the generated ToolHandlerFor wrapper): only a
// *jsonrpc.Error return becomes a protocol-level error; every other error is
// routed through CallToolResult.SetError, which is EXACTLY the
// {IsError: true, Content: [text]} shape the operator asked for. A hand-built
// *mcp.CallToolResult carrying a zero Out is worse, not safer: with the error
// short-circuit skipped it falls through to output-schema validation
// (server.go:405-424), which fails a struct Out's required fields with a real
// protocol error — precisely the outcome this guard exists to avoid. See this
// plan's <falsified_premise> block for the full citation chain.
func identityFromMeta(req *mcp.CallToolRequest) (string, error) {
	if req == nil || req.Params == nil {
		return "", errMissingIdentityMeta
	}
	meta := req.Params.GetMeta()
	aura, ok := meta[auraMetaKey].(map[string]any)
	if !ok {
		return "", errMissingIdentityMeta
	}
	raw, ok := aura[metaFieldUserIdentifier]
	if !ok {
		return "", errMissingIdentityMeta
	}
	identity, ok := raw.(string)
	if !ok {
		return "", errMissingIdentityMeta
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return "", errMissingIdentityMeta
	}
	return identity, nil
}

// resolveCaller is identityFromMeta plus the tenant lookup every handler but
// memory_recall needs immediately after it — extracted because the five-line
// "identity, err := identityFromMeta(req); ...; client, err := tenants.For(...)"
// sequence repeated verbatim across eight handlers, and CLAUDE.md's
// reusable-code rule (and the CI dupl gate, which caught the two nearest
// instances in tool_browse.go) forbids that. Order is unchanged: identity
// first (fail closed on absent/malformed identity before any tenant is ever
// touched), tenant resolution second. Returns identity too, not just client,
// because memory_upsert_fact's canonicalSubject also needs it.
func resolveCaller(ctx context.Context, tenants *tenants, req *mcp.CallToolRequest) (identity string, client *arcadedb.Client, err error) {
	identity, err = identityFromMeta(req)
	if err != nil {
		return "", nil, err
	}
	client, err = tenants.For(ctx, identity)
	if err != nil {
		return "", nil, err
	}
	return identity, client, nil
}
