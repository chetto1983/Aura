package openai_compat

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type wireTurn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID string `json:"id"`
	} `json:"tool_calls"`
}

func wireTurns(t *testing.T, messages []llm.Message) []wireTurn {
	t.Helper()
	wire, _, err := toSDKMessages(messages, nil, nil, llm.ReasoningTargetNone)
	if err != nil {
		t.Fatalf("toSDKMessages: %v", err)
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var turns []wireTurn
	if err := json.Unmarshal(raw, &turns); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return turns
}

func rolesOf(turns []wireTurn) []string {
	roles := make([]string, len(turns))
	for i, turn := range turns {
		roles[i] = turn.Role
	}
	return roles
}

func assistant(content string, callIDs ...string) llm.Message {
	message := llm.Message{Role: llm.RoleAssistant, Content: content}
	for _, id := range callIDs {
		message.ToolCalls = append(message.ToolCalls, llm.ToolCall{ID: id, Type: "function"})
	}
	return message
}

func TestAdjacentAssistantTurnsLeaveAsOne(t *testing.T) {
	system := llm.Message{Role: llm.RoleSystem, Content: "sys"}
	question := llm.Message{Role: llm.RoleUser, Content: "question"}
	result := llm.Message{Role: llm.RoleTool, ToolCallID: "call_1", Content: "result"}

	tests := []struct {
		name      string
		messages  []llm.Message
		roles     []string
		at        int
		content   string
		toolCalls []string
	}{
		{
			name:     "two records between two questions",
			messages: []llm.Message{system, question, assistant("first"), assistant("second"), question},
			roles:    []string{"system", "user", "assistant", "user"},
			at:       2, content: "first\n\nsecond",
		},
		{
			name:     "a thread the watch opened",
			messages: []llm.Message{system, assistant("one"), assistant("two"), assistant("three"), question},
			roles:    []string{"system", "assistant", "user"},
			at:       1, content: "one\n\ntwo\n\nthree",
		},
		{
			name:     "a record before the model's call keeps the call",
			messages: []llm.Message{question, assistant("record"), assistant("", "call_1"), result, assistant("done")},
			roles:    []string{"user", "assistant", "tool", "assistant"},
			at:       1, content: "record", toolCalls: []string{"call_1"},
		},
		{
			name:     "a record between a call and its result keeps the pair valid",
			messages: []llm.Message{question, assistant("", "call_1"), assistant("record"), result},
			roles:    []string{"user", "assistant", "tool"},
			at:       1, content: "record", toolCalls: []string{"call_1"},
		},
		{
			name:     "both sides calling keep every call",
			messages: []llm.Message{question, assistant("a", "call_1"), assistant("b", "call_2")},
			roles:    []string{"user", "assistant"},
			at:       1, content: "a\n\nb", toolCalls: []string{"call_1", "call_2"},
		},
		{
			name:     "a tool round keeps its two assistant turns apart",
			messages: []llm.Message{question, assistant("", "call_1"), result, assistant("done")},
			roles:    []string{"user", "assistant", "tool", "assistant"},
			at:       3, content: "done",
		},
		{
			name:     "the always-block stays its own user turn",
			messages: []llm.Message{system, {Role: llm.RoleUser, Content: "always"}, question},
			roles:    []string{"system", "user", "user"},
			at:       1, content: "always",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			turns := wireTurns(t, test.messages)
			if got := rolesOf(turns); !reflect.DeepEqual(got, test.roles) {
				t.Fatalf("roles = %v, want %v", got, test.roles)
			}
			turn := turns[test.at]
			if turn.Content != test.content {
				t.Fatalf("content = %q, want %q", turn.Content, test.content)
			}
			var calls []string
			for _, call := range turn.ToolCalls {
				calls = append(calls, call.ID)
			}
			if !reflect.DeepEqual(calls, test.toolCalls) {
				t.Fatalf("tool calls = %v, want %v", calls, test.toolCalls)
			}
		})
	}
}

func TestJoiningLeavesTheStoredTurnsAlone(t *testing.T) {
	first := assistant("first", "call_1")
	first.ToolCalls = append(first.ToolCalls[:1:1], llm.ToolCall{})[:1]
	messages := []llm.Message{first, assistant("second", "call_2")}

	joinAssistantRuns(messages)

	if messages[0].Content != "first" {
		t.Fatalf("the first turn's text moved: %q", messages[0].Content)
	}
	if spare := messages[0].ToolCalls[:2]; spare[1].ID != "" {
		t.Fatalf("the first turn's backing array was written through: %+v", spare[1])
	}
	if len(messages[0].ToolCalls) != 1 {
		t.Fatalf("the first turn's calls grew: %+v", messages[0].ToolCalls)
	}
}
