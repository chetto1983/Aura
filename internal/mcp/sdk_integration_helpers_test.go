//go:build calendar_integration || whatsapp_integration

// Shared harness for the two build-tagged live-sidecar integration tiers
// (D-110's "prove the real wire" tier). Both tags are given together by the
// verification command (go vet -tags='calendar_integration whatsapp_integration'
// ./internal/mcp/), so their files compile into the same package build and cannot
// each define their own copy of this helper — hence one shared, explicitly gated
// file rather than two near-duplicates.
package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// drainSDKToolsForTest lists a REAL sidecar's tools through DrainTools, the production
// path, so pagination is checked against the sidecar and not just a fixture.
func drainSDKToolsForTest(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession) []*sdkmcp.Tool {
	t.Helper()
	tools, err := DrainTools(ctx, session)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	return tools
}

// callAndDecodeForTest calls tool on session and decodes its result through the
// SAME domain-outcome chain (DecodeToolResult, UNCHANGED logic) every production
// caller uses — mirrors bridge_supervisor.go's decodeResult / cmd/aura's
// callSessionText.
func callAndDecodeForTest(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, tool string, args map[string]any) (text string, isError bool) {
	t.Helper()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", tool, err)
	}
	return DecodeToolResult(res)
}
