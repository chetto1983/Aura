package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestCallToolRejectsExplicitDomainFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		result string
	}{
		{
			name: "structured content",
			result: `{
				"content":[{"type":"text","text":"Failed to download media"}],
				"structuredContent":{"success":false,"message":"Failed to download media"},
				"isError":false
			}`,
		},
		{
			name: "text JSON",
			result: `{
				"content":[{"type":"text","text":"{\"success\":false,\"message\":\"Failed to download media\"}"}],
				"isError":false
			}`,
		},
		{
			name: "top-level error field",
			result: `{
				"content":[{"type":"text","text":"{\"error\":\"entity not found\"}"}],
				"isError":false
			}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			roundtrip := func(context.Context, string, any) (json.RawMessage, error) {
				return json.RawMessage(tc.result), nil
			}
			_, err := callToolWith(t.Context(), "whatsapp", "download_media", nil, roundtrip)
			var toolErr *ToolCallError
			if !errors.As(err, &toolErr) {
				t.Fatalf("err = %v, want *ToolCallError", err)
			}
		})
	}
}

func TestCallToolPreservesFalseDataFields(t *testing.T) {
	t.Parallel()

	roundtrip := func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{
			"content":[{"type":"text","text":"{\"resolved\":false}"}],
			"structuredContent":{"resolved":false},
			"isError":false
		}`), nil
	}
	text, err := callToolWith(t.Context(), "whatsapp", "get_contact", nil, roundtrip)
	if err != nil {
		t.Fatalf("callToolWith: %v", err)
	}
	if text != `{"resolved":false}` {
		t.Fatalf("text = %q", text)
	}
}
