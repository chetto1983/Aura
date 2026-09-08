package agui

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestRecoveredToolResultIsNotProjectedAsSuccess(t *testing.T) {
	var call llm.ToolCall
	call.ID, call.Type, call.Function.Name = "recovered-call", "function", "shell_exec"
	unknown := `error: previous result unknown after crash recovery for tool "shell_exec"; verify before re-running this tool call.`
	for _, content := range []string{unknown, "9137", "error: ordinary command output"} {
		history := []llm.Message{
			{Role: llm.RoleUser, Content: unknown},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
			{Role: llm.RoleTool, ToolCallID: call.ID, Content: content},
		}
		encoded, err := json.Marshal(projectDisplaySnapshot(history))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Messages []struct {
				Content string `json:"content"`
				IsError bool   `json:"isError"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Messages[0].IsError || decoded.Messages[2].IsError != (content == unknown) {
			t.Fatalf("incorrect recovery outcome: %s", encoded)
		}
		if decoded.Messages[2].Content != content {
			t.Fatalf("result changed: %s", encoded)
		}
	}
}
