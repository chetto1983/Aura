package conversations

import (
	"testing"
)

func TestProjectionTurnEligibility(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		content    string
		toolCallID string
		toolCalls  []byte
		want       bool
	}{
		{name: "user natural language", role: "user", content: "Remember the blue notebook", want: true},
		{name: "final assistant answer", role: "assistant", content: "The notebook is on the desk.", want: true},
		{name: "system", role: "system", content: "system prompt"},
		{name: "compaction summary", role: "system", content: "Summary of earlier turns"},
		{name: "tool result", role: "tool", content: `{"secret":"raw"}`, toolCallID: "call-1"},
		{name: "assistant tool call", role: "assistant", toolCalls: []byte(`[{"id":"call-1"}]`)},
		{name: "assistant tool call with preview", role: "assistant", content: "calling tool", toolCalls: []byte(`[{"id":"call-1"}]`)},
		{name: "reasoning-only assistant", role: "assistant", content: "   "},
		{name: "blank user", role: "user", content: "\n\t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := projectionTurnEligible(tt.role, tt.content, tt.toolCallID, tt.toolCalls); got != tt.want {
				t.Fatalf("projectionTurnEligible() = %t, want %t", got, tt.want)
			}
		})
	}
}
