package telegram

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestDebugTextSmokeResultFromMessagesDetectsExecuteCodeAnd5050(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Name: "execute_code",
				Arguments: map[string]any{
					"code": "print(sum(range(1, 101)))",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content:    "exit_code: 0\nelapsed_ms: 42\n\n5050",
		},
		{
			Role:    "assistant",
			Content: "The result is 5050.",
		},
	})

	if !result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = false, want true")
	}
	if !result.Contains5050 {
		t.Fatal("Contains5050 = false, want true")
	}
	if result.FinalText != "The result is 5050." {
		t.Fatalf("FinalText = %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0] != "execute_code" {
		t.Fatalf("ToolCalls = %v", result.ToolCalls)
	}
}

func TestDebugTextSmokeResultFromMessagesReportsMissingTool(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{Role: "assistant", Content: "5050"},
	})

	if result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = true, want false")
	}
	if !result.Contains5050 {
		t.Fatal("Contains5050 = false, want true from final text")
	}
}
