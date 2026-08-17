package mcp

import (
	"encoding/json"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// result.go re-expresses the pre-SDK decodeToolResult chain (client.go:392-421,
// deleted in plan 45.1-03) against the SDK's typed CallToolResult. tool_methods.go
// (also deleted in 45.1-03) was the only caller of that chain;
// bridge_supervisor.go's CallToolText is now the single call site anywhere in the
// tree (RESEARCH Pitfall 1).

// ToolPayload is one tools/call result decoded into the two projections Aura
// actually consumes: the concatenated text the MODEL reads, and the structured
// payload a VIEW reads (MCP Apps hands `structuredContent` to the rendered
// document in ui/notifications/tool-result).
//
// They are one result, so they are decoded together. A caller that needed only
// the second would otherwise re-walk the first, and the two would drift.
type ToolPayload struct {
	Text string
	// Structured is the server's structuredContent as raw JSON, nil when it sent
	// none. Raw rather than `any` because every consumer downstream of here
	// either forwards it verbatim or re-marshals it.
	Structured json.RawMessage
}

// DecodeToolResult extracts the concatenated text content and error flag from an
// SDK tools/call result. Non-text content parts (images, resource links, ...) are
// skipped rather than stringified — mirrors decodeToolResult's old
// content[].type=="text" filter, just against typed fields instead of a raw JSON
// envelope. isError escalates from false to true when the result's structured
// content (or, failing that, its text re-parsed as JSON) carries an explicit
// domain failure (explicitDomainFailure, UNCHANGED) even though the server
// reported IsError:false — the false-negative case the pre-SDK chain also caught.
func DecodeToolResult(result *sdkmcp.CallToolResult) (text string, isError bool) {
	payload, isError := DecodeToolPayload(result)
	return payload.Text, isError
}

// DecodeToolPayload is DecodeToolResult plus the structured payload, decoded in
// the same pass. DecodeToolResult is the text-only projection of it, kept because
// most callers want exactly that and should not carry a field they ignore.
func DecodeToolPayload(result *sdkmcp.CallToolResult) (payload ToolPayload, isError bool) {
	if result == nil {
		return ToolPayload{}, false
	}
	var b strings.Builder
	for _, part := range result.Content {
		if tc, ok := part.(*sdkmcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	payload = ToolPayload{
		Text:       strings.TrimRight(b.String(), "\n"),
		Structured: structuredJSON(result),
	}
	isError = result.IsError
	if !isError && explicitDomainFailure(payload.Structured, payload.Text) {
		isError = true
	}
	return payload, isError
}

// structuredJSON marshals a result's structuredContent. A marshal failure is
// treated as "no structured content" rather than failing the call outright — the
// text leg of explicitDomainFailure still runs, and a view simply renders nothing
// instead of the whole call erroring on a payload only the view would have read.
func structuredJSON(result *sdkmcp.CallToolResult) json.RawMessage {
	if result.StructuredContent == nil {
		return nil
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil
	}
	return raw
}
