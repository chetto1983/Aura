package mcptools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/pausable"
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
	browser := b.policy.recipeSource == mcpmanager.BrowserRecipeSource
	if browser {
		if err := validateBrowserArguments(b.name, args); err != nil {
			observeErr = err
			return tools.ToolResult{}, err
		}
		profiled, err := withBrowserProfile(args)
		if err != nil {
			observeErr = err
			return tools.ToolResult{}, err
		}
		args = profiled
		if err := browserGuard(b.srv.browser, browserIdentity(ctx), b.name, args); err != nil {
			observeErr = err
			return tools.ToolResult{}, err
		}
	}
	timeout := b.callTimeout
	if ceiling, ok := tools.CallCeiling(ctx); ok {
		// The agent loop stopped waiting at its window and moved the call to the
		// background; the bound left is the one it gives every backgrounded call.
		timeout = ceiling
	}
	callCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		// The operator's time is excluded while the server's form waits.
		callCtx, cancel = pausable.WithTimeout(ctx, timeout)
	}
	defer cancel()

	var madeRoom string
	if browser {
		room, err := b.makeBrowserRoom(callCtx, args)
		if err != nil {
			observeErr = err
			return tools.ToolResult{}, err
		}
		madeRoom = room
	}
	payload, err := b.srv.CallTool(callCtx, b.name, args)
	var downloadFooter string
	if browser {
		downloadFooter = b.downloadFooter(callCtx)
	}
	if err != nil {
		observeErr = err
		if downloadFooter != "" {
			return tools.ToolResult{}, fmt.Errorf("%w%s", boundedMCPError(err), downloadFooter)
		}
		return tools.ToolResult{}, boundedMCPError(err)
	}
	if browser {
		refreshBrowserReferences(b.srv.browser, browserIdentity(ctx), args, payload.Text)
		payload.Text += madeRoom + b.browserCallDone(ctx, args)
	}
	return b.newResultWithTail(ctx, args, payload, downloadFooter)
}

// newResultWithTail wraps an MCP tool's output. A mounted MCP server is
// operator-configured infrastructure, so its output is marked TrustTrusted:
// trusted content like a built-in, never wrapped in the untrusted envelope. The
// preview cap and sidecar spillover (resultText) still bound it; only the distrust
// framing is dropped.
//
// A view-bound tool additionally carries the MCP Apps descriptor on the result's
// Meta. The MODEL never sees it — Meta is not part of the preview threaded back
// into history — so a server cannot use the view channel to say something extra
// to the model; it reaches only the surfaces that render (bridge_views.go).
//
// tail, when non-empty, follows the payload text and its files footer, and survives
// the preview cap (tools.NewResultReservingTail).
func (b *bridgedTool) newResultWithTail(ctx context.Context, args map[string]any, payload mcp.ToolPayload, tail string) (tools.ToolResult, error) {
	res, err := b.resultTextWithTail(ctx, payload, tail)
	if err != nil {
		return tools.ToolResult{}, err
	}
	res.Provenance = &tools.ToolResultProvenance{
		Source: "mcp:" + b.Spec().Name,
		Trust:  tools.TrustTrusted,
	}
	if marker, ok := b.displaySource(args); ok {
		if res.Meta == nil {
			res.Meta = &tools.ToolResultMeta{}
		}
		(*res.Meta)["aura_display_source"] = marker
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

// resultTextWithTail is what the model reads: the server's text, then what became of any
// files the result carried. The footer is reserved from the preview cap, because a
// long email body must not truncate away the path to its attachment.
func (b *bridgedTool) resultTextWithTail(ctx context.Context, payload mcp.ToolPayload, tail string) (tools.ToolResult, error) {
	if len(payload.Files) == 0 && tail == "" {
		return tools.NewResult(ctx, payload.Text)
	}
	footer := tail
	if len(payload.Files) > 0 {
		footer = b.srv.filesFooter(ctx, payload.Files) + tail
	}
	if payload.Text != "" {
		footer = "\n\n" + footer
	}
	return tools.NewResultReservingTail(ctx, payload.Text, footer)
}
