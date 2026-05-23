package governance

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestScrubOrphanToolCalls_DropsOrphanToolResult(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "file"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "ok"},
		// orphan: "xyz" was never declared by any assistant message
		{Role: "tool", ToolCallID: "xyz", Content: "orphan result"},
	}
	out := ScrubOrphanToolCalls(history)
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (orphan should be stripped)", len(out))
	}
	for _, msg := range out {
		if msg.ToolCallID == "xyz" {
			t.Fatalf("orphan tool result with id=xyz survived ScrubOrphanToolCalls: %+v", msg)
		}
	}
}

func TestScrubOrphanToolCalls_BackfillsMissingResult(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "go"},
		// assistant declared "abc" but never got a result
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "abc", Name: "search_memory"}}},
	}
	out := ScrubOrphanToolCalls(history)
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (placeholder should be inserted for abc)", len(out))
	}
	var found bool
	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "abc" {
			found = true
			if !strings.Contains(msg.Content, "interrupted") {
				t.Fatalf("backfilled tool result content = %q, want interrupted marker", msg.Content)
			}
		}
	}
	if !found {
		t.Fatal("backfilled synthetic tool result with tool_call_id='abc' not found")
	}
}

func TestScrubOrphanToolCalls_NoChangeOnCleanHistory(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "file"}}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
	}
	out := ScrubOrphanToolCalls(history)
	if len(out) != len(history) {
		t.Fatalf("len(out) = %d, want %d (clean history should not change)", len(out), len(history))
	}
}

func TestScrubOrphanToolCalls_CombinedOrphanAndBackfill(t *testing.T) {
	// "declared" has no result (backfill needed); "ghost" has no matching call (drop needed).
	// Ordering: dropOrphan runs first, then backfill.
	// dropOrphan: removes "ghost" → [user, assistant("declared")]
	// backfill: inserts placeholder for "declared" → [user, assistant, placeholder]
	history := []llm.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "declared", Name: "file"}}},
		{Role: "tool", ToolCallID: "ghost", Content: "orphan"},
	}
	out := ScrubOrphanToolCalls(history)
	// Result: 3 messages (user + assistant + backfilled placeholder)
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (ghost dropped, declared backfilled)", len(out))
	}
	for _, msg := range out {
		if msg.ToolCallID == "ghost" {
			t.Fatal("orphan 'ghost' should have been dropped")
		}
	}
	var backfilled bool
	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "declared" {
			backfilled = true
		}
	}
	if !backfilled {
		t.Fatal("placeholder for 'declared' should have been backfilled")
	}
}
