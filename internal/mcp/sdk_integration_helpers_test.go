//go:build calendar_integration || whatsapp_integration || calculator_integration

// Shared harness for the three build-tagged live-sidecar integration tiers
// (D-110's "prove the real wire" tier). All three tags are given together by the
// verification command (go vet -tags='calendar_integration whatsapp_integration
// calculator_integration' ./internal/mcp/), so their files compile into the same
// package build and cannot each define their own copy of this helper — hence one
// shared, explicitly gated file rather than three near-duplicates.
package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// drainSDKToolsForTest drains session's paginated tool list. Mirrors probe.go's
// countTools and cmd/aura's drainSDKTools — pagination is a capability the
// hand-rolled client never had, and it is checked here against a REAL sidecar,
// not just a fixture.
func drainSDKToolsForTest(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession) []*sdkmcp.Tool {
	t.Helper()
	var out []*sdkmcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		out = append(out, tool)
	}
	return out
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
