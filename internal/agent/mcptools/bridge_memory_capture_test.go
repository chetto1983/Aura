package mcptools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMemoryUpsertAcceptedCapture(t *testing.T) {
	ctx := tools.WithRequestID(context.Background(), "run-parent")
	args := map[string]any{
		"subject": "operator", "predicate": "lives_in", "object": "Torino",
		"statement": "The operator lives in Torino.",
		"source":    map[string]any{"memory_ids": []any{"message-7"}},
	}
	payload := mcp.ToolPayload{Structured: json.RawMessage(`{"statement":"The operator lives in Torino.","superseded":0,"refused":false}`)}

	evidence, ok := acceptedFactEvidence(ctx, "memory_upsert_fact", args, payload)
	if !ok {
		t.Fatal("successful memory_upsert_fact structured result emitted no evidence")
	}
	if evidence.Subject != "operator" || evidence.Predicate != "lives_in" || evidence.Object != "Torino" ||
		evidence.Statement != "The operator lives in Torino." || evidence.ActorRunID != "run-parent" || evidence.ActorRole != "parent" {
		t.Fatalf("evidence = %+v", evidence)
	}

	for _, tc := range []struct {
		name    string
		tool    string
		payload mcp.ToolPayload
	}{
		{name: "different tool", tool: "memory_recall", payload: payload},
		{name: "refused", tool: "memory_upsert_fact", payload: mcp.ToolPayload{Structured: json.RawMessage(`{"statement":"x","refused":true}`)}},
		{name: "unstructured", tool: "memory_upsert_fact", payload: mcp.ToolPayload{Text: "looks successful"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, accepted := acceptedFactEvidence(ctx, tc.tool, args, tc.payload); accepted {
				t.Fatalf("ineligible result emitted evidence: %+v", got)
			}
		})
	}
}
