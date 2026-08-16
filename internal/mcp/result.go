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

// DecodeToolResult extracts the concatenated text content and error flag from an
// SDK tools/call result. Non-text content parts (images, resource links, ...) are
// skipped rather than stringified — mirrors decodeToolResult's old
// content[].type=="text" filter, just against typed fields instead of a raw JSON
// envelope. isError escalates from false to true when the result's structured
// content (or, failing that, its text re-parsed as JSON) carries an explicit
// domain failure (explicitDomainFailure, UNCHANGED) even though the server
// reported IsError:false — the false-negative case the pre-SDK chain also caught.
func DecodeToolResult(result *sdkmcp.CallToolResult) (text string, isError bool) {
	if result == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range result.Content {
		if tc, ok := part.(*sdkmcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	text = strings.TrimRight(b.String(), "\n")
	isError = result.IsError
	if !isError {
		var structured json.RawMessage
		if result.StructuredContent != nil {
			// A marshal failure is treated as "no structured content" rather than
			// failing the call outright — the text leg of explicitDomainFailure
			// still runs against the concatenated text above.
			if raw, err := json.Marshal(result.StructuredContent); err == nil {
				structured = raw
			}
		}
		if explicitDomainFailure(structured, text) {
			isError = true
		}
	}
	return text, isError
}
