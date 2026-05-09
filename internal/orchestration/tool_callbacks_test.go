package orchestration

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
)

func TestDefaultBeforeToolSkipsRepeatedSearchMemory(t *testing.T) {
	before := BeforeToolCallbackForToolset(ToolsetDefault)
	if before == nil {
		t.Fatal("BeforeToolCallbackForToolset(default) = nil")
	}

	decision := before(llm.ToolCall{Name: "search_memory"}, agentloop.ToolCallState{CallsForTool: 1})
	if !decision.Skip {
		t.Fatal("default repeated search_memory was not skipped")
	}
	if !strings.Contains(decision.Result, "answer now") {
		t.Fatalf("skip result = %q, want answer-now guidance", decision.Result)
	}
}

func TestDefaultAfterToolAsksModelToAnswerAfterMemory(t *testing.T) {
	after := AfterToolCallbackForToolset(ToolsetDefault)
	if after == nil {
		t.Fatal("AfterToolCallbackForToolset(default) = nil")
	}

	result := after(llm.ToolCall{Name: "search_memory"}, "evidence", nil)
	if !strings.Contains(result, "evidence") || !strings.Contains(result, "Answer now") || !strings.Contains(result, "do not call search_memory again") || !strings.Contains(result, "another currently exposed tool") {
		t.Fatalf("default search_memory result missing finalization hint: %q", result)
	}
}
