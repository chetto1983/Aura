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
		{
			name: "null deletion with reason",
			result: `{
				"content":[{"type":"text","text":"{\"deleted\":null,\"reason\":\"forgetting a relationship needs source_id, target_id and relationship_type\"}"}],
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

	cases := []string{
		`{"resolved":false}`,
		`{"deleted":null}`,
		`{"reason":"informational"}`,
		`{"deleted":"relationship-id","reason":"deleted"}`,
	}
	for _, payload := range cases {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()
			roundtrip := func(context.Context, string, any) (json.RawMessage, error) {
				result, err := json.Marshal(map[string]any{
					"content": []map[string]any{{
						"type": "text",
						"text": payload,
					}},
					"structuredContent": json.RawMessage(payload),
					"isError":           false,
				})
				return result, err
			}
			text, err := callToolWith(t.Context(), "whatsapp", "get_contact", nil, roundtrip)
			if err != nil {
				t.Fatalf("callToolWith: %v", err)
			}
			if text != payload {
				t.Fatalf("text = %q, want %q", text, payload)
			}
		})
	}
}
