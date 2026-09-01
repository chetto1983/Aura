package mcptools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMemoryUpsertAcceptedCapture(t *testing.T) {
	ctx := tools.WithRequestID(context.Background(), "run-parent")
	args := map[string]any{
		"subject": "operator", "predicate": "lives_in", "object": "Torino",
		"statement":  "The operator lives in Torino.",
		"valid_from": "2026-09-01T01:02:03Z", "supersedes": true,
		"supersedes_fact_key": strings.Repeat("a", 64),
		"source":              map[string]any{"memory_ids": []any{"message-7"}},
	}
	payload := mcp.ToolPayload{Structured: json.RawMessage(`{"statement":"The operator lives in Torino.","superseded":0,"refused":false}`)}

	evidence, ok := acceptedFactEvidence(ctx, memoryUpsertFactModelName, args, payload)
	if !ok {
		t.Fatal("successful memory_upsert_fact structured result emitted no evidence")
	}
	if evidence.Subject != "operator" || evidence.Predicate != "lives_in" || evidence.Object != "Torino" ||
		evidence.Statement != "The operator lives in Torino." || evidence.ActorRunID != "run-parent" || evidence.ActorRole != "parent" ||
		evidence.ValidFrom != "2026-09-01T01:02:03Z" || !evidence.Supersedes ||
		evidence.SupersedesFactKey != strings.Repeat("a", 64) {
		t.Fatalf("evidence = %+v", evidence)
	}

	tool := &bridgedTool{}
	tool.storeSpec(tools.Spec{Name: memoryUpsertFactModelName})
	callCtx := tools.WithToolCallContext(ctx, "conversation-a", "call-memory", t.TempDir(), 4096)
	result, err := tool.newResult(callCtx, args, payload)
	if err != nil {
		t.Fatalf("newResult: %v", err)
	}
	if result.Meta == nil {
		t.Fatal("production bridge result has nil Meta")
	}
	if got, typed := (*result.Meta)[tools.MetaAcceptedFact].(tools.AcceptedFactEvidence); !typed || !reflect.DeepEqual(got, evidence) {
		t.Fatalf("production bridge evidence = %#v, want %+v", (*result.Meta)[tools.MetaAcceptedFact], evidence)
	}

	for _, tc := range []struct {
		name    string
		tool    string
		payload mcp.ToolPayload
	}{
		{name: "different tool", tool: "memory_recall", payload: payload},
		{name: "refused", tool: memoryUpsertFactModelName, payload: mcp.ToolPayload{Structured: json.RawMessage(`{"statement":"x","refused":true}`)}},
		{name: "unstructured", tool: memoryUpsertFactModelName, payload: mcp.ToolPayload{Text: "looks successful"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, accepted := acceptedFactEvidence(ctx, tc.tool, args, tc.payload); accepted {
				t.Fatalf("ineligible result emitted evidence: %+v", got)
			}
		})
	}
}
