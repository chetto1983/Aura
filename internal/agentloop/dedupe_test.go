package agentloop

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestDedupeToolCallsSkipsIdenticalCalls(t *testing.T) {
	calls := []llm.ToolCall{
		{ID: "1", Name: "search_files", Arguments: map[string]any{"query": "agent loop"}},
		{ID: "2", Name: "search_files", Arguments: map[string]any{"query": "memory"}},
		{ID: "3", Name: "search_files", Arguments: map[string]any{"query": "agent loop"}},
		{ID: "4", Name: "read_file", Arguments: map[string]any{"path": "wiki/agent-loop.md"}},
	}

	kept, duplicates := DedupeToolCalls(calls)

	if len(kept) != 3 || kept[0].ID != "1" || kept[1].ID != "2" || kept[2].ID != "4" {
		t.Fatalf("kept calls = %+v", kept)
	}
	if len(duplicates) != 1 || duplicates[0].ID != "3" {
		t.Fatalf("duplicates = %+v", duplicates)
	}
}

func TestDedupeToolCallsCanonicalizesArgumentOrder(t *testing.T) {
	calls := []llm.ToolCall{
		{ID: "1", Name: "search_files", Arguments: map[string]any{"query": "agent loop", "limit": 5}},
		{ID: "2", Name: "search_files", Arguments: map[string]any{"limit": 5, "query": "agent loop"}},
	}

	kept, duplicates := DedupeToolCalls(calls)

	if len(kept) != 1 || kept[0].ID != "1" {
		t.Fatalf("kept calls = %+v", kept)
	}
	if len(duplicates) != 1 || duplicates[0].ID != "2" {
		t.Fatalf("duplicates = %+v", duplicates)
	}
}
