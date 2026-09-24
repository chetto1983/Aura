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
// MountedServer, and threads the result through resultText: tools.NewResult when it
// is text alone, tools.NewResultReservingTail when files travelled with it. Execute
// does not decode results itself — b.srv.CallTool does, through the ONE
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
	return b.newResult(ctx, args, payload)
}

// newResult wraps an MCP tool's output. A mounted MCP server is
// operator-configured infrastructure, so its output is marked TrustTrusted:
// trusted content like a built-in, never wrapped in the untrusted envelope. The
// preview cap and sidecar spillover (resultText) still bound it; only the distrust
// framing is dropped.
//
// A view-bound tool additionally carries the MCP Apps descriptor on the result's
// Meta. The MODEL never sees it — Meta is not part of the preview threaded back
// into history — so a server cannot use the view channel to say something extra
// to the model; it reaches only the surfaces that render (bridge_views.go).
func (b *bridgedTool) newResult(ctx context.Context, args map[string]any, payload mcp.ToolPayload) (tools.ToolResult, error) {
	res, err := b.resultText(ctx, payload)
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
	if evidence, ok := acceptedFactEvidence(ctx, b.Spec().Name, args, payload); ok {
		if res.Meta == nil {
			res.Meta = &tools.ToolResultMeta{}
		}
		(*res.Meta)[tools.MetaAcceptedFact] = evidence
	}
	return res, nil
}

// resultText is what the model reads: the server's text, then what became of any
// files the result carried. The footer is reserved from the preview cap, because a
// long email body must not truncate away the path to its attachment.
func (b *bridgedTool) resultText(ctx context.Context, payload mcp.ToolPayload) (tools.ToolResult, error) {
	if len(payload.Files) == 0 {
		return tools.NewResult(ctx, payload.Text)
	}
	footer := b.srv.filesFooter(ctx, payload.Files)
	if payload.Text != "" {
		footer = "\n\n" + footer
	}
	return tools.NewResultReservingTail(ctx, payload.Text, footer)
}
