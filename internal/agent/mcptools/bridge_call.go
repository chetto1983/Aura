package mcptools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/obs"
)

// bridge_call.go holds bridgedTool's call path: Execute is the natural seam
// bridge.go's spec-shaping side splits from at the ≤600 LOC refactor-on-touch
// threshold (CLAUDE.md NO GOD CLASS), and it is what RESEARCH names as the
// tool-registry-adaptation call site — not a facade, the call site itself.

var mcpBridgeBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/internal/agent/mcptools", obs.BoundaryConfig{
	Operation: "mcp_bridge", ToolClass: obs.ToolClassMCP, Transport: "in_process",
	Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
})

// Execute unmarshals the model's args, calls the MCP tool through the mounted
// MountedServer, and threads successful text through tools.NewResult. Execute
// does not decode results itself — b.srv.CallToolText does, through the ONE
// result-decode call site in the tree — so an MCP isError=true (or a transport
// failure) remains a Go error the agent loop can render as an error observation
// without completing idempotency as success.
func (b *bridgedTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	ctx, end := mcpBridgeBoundary.Start(ctx)
	var observeErr error
	defer end.PanicSafe(&observeErr)
	var args map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			observeErr = err
			return tools.ToolResult{}, fmt.Errorf("mcp tool %s args: %w", b.name, err)
		}
	}
	callCtx := ctx
	cancel := func() {}
	if b.callTimeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, b.callTimeout)
	}
	defer cancel()

	payload, err := b.srv.CallTool(callCtx, b.name, args)
	if err != nil {
		observeErr = err
		return tools.ToolResult{}, boundedMCPError(err)
	}
	return b.newResult(ctx, payload)
}

// newResult wraps an MCP tool's output. A mounted MCP server is
// operator-configured infrastructure, so its output is marked TrustTrusted:
// trusted content like a built-in, never wrapped in the untrusted envelope. Size
// caps in tools.NewResult still bound it; only the distrust framing is dropped.
//
// A view-bound tool additionally carries the MCP Apps descriptor on the result's
// Meta. The MODEL never sees it — Meta is not part of the preview threaded back
// into history — so a server cannot use the view channel to say something extra
// to the model; it reaches only the surfaces that render (bridge_views.go).
func (b *bridgedTool) newResult(ctx context.Context, payload mcp.ToolPayload) (tools.ToolResult, error) {
	res, err := tools.NewResult(ctx, payload.Text)
	if err != nil {
		return tools.ToolResult{}, err
	}
	res.Provenance = &tools.ToolResultProvenance{
		Source: "mcp:" + b.Spec().Name,
		Trust:  tools.TrustTrusted,
	}
	if descriptor, ok := b.viewDescriptor(payload); ok {
		if res.Meta == nil {
			res.Meta = &tools.ToolResultMeta{}
		}
		(*res.Meta)[viewMetaKey] = descriptor
	}
	return res, nil
}
