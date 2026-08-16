package mcp

import (
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// domain_outcome_test.go exercises explicitDomainFailure (UNCHANGED logic) through
// its SDK-era call site: DecodeToolResult -> DecodeToolCallError, the chain
// bridge_supervisor.go's decodeResult and cmd/aura's callSessionText both use. The
// pre-SDK glue this used to drive directly (callToolWith, tool_methods.go) is
// deleted in plan 45.1-03; the fixtures below are unchanged, only the entry point
// moved to typed *sdkmcp.CallToolResult fields instead of a raw JSON envelope.

func TestDecodeToolResultRejectsExplicitDomainFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		text       string
		structured any
	}{
		{
			name:       "structured content",
			text:       "Failed to download media",
			structured: map[string]any{"success": false, "message": "Failed to download media"},
		},
		{
			name: "text JSON",
			text: `{"success":false,"message":"Failed to download media"}`,
		},
		{
			name: "top-level error field",
			text: `{"error":"entity not found"}`,
		},
		{
			name: "null deletion with reason",
			text: `{"deleted":null,"reason":"forgetting a relationship needs source_id, target_id and relationship_type"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := &sdkmcp.CallToolResult{
				Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: tc.text}},
				StructuredContent: tc.structured,
			}
			text, isErr := DecodeToolResult(result)
			if !isErr {
				t.Fatalf("isError = false, want true (explicit domain failure) for %q", tc.text)
			}
			if toolErr := DecodeToolCallError("whatsapp", "download_media", text); toolErr == nil {
				t.Fatal("DecodeToolCallError returned nil for an explicit domain failure")
			}
		})
	}
}

func TestDecodeToolResultPreservesFalseDataFields(t *testing.T) {
	t.Parallel()

	cases := []string{
		`{"resolved":false}`,
		`{"deleted":null}`,
		`{"reason":"informational"}`,
		`{"deleted":"relationship-id","reason":"deleted"}`,
	}
	for _, payload := range cases {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()
			var structured any
			if err := json.Unmarshal([]byte(payload), &structured); err != nil {
				t.Fatalf("unmarshal fixture payload: %v", err)
			}
			result := &sdkmcp.CallToolResult{
				Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: payload}},
				StructuredContent: structured,
			}
			text, isErr := DecodeToolResult(result)
			if isErr {
				t.Fatalf("isError = true, want false for %q", payload)
			}
			if text != payload {
				t.Fatalf("text = %q, want %q", text, payload)
			}
		})
	}
}
