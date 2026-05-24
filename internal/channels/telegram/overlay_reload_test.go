package telegramadapter

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestOverlayWriteInCalls(t *testing.T) {
	tests := []struct {
		name  string
		calls []llm.ToolCall
		want  bool
	}{
		{
			name:  "write to retired TOOLS.md ignored",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "write", "path": "TOOLS.md", "content": "x"}}},
			want:  false,
		},
		{
			name:  "patch to SOUL.md detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "patch", "path": "SOUL.md", "old": "a", "new": "b"}}},
			want:  true,
		},
		{
			name:  "write to USER.md detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "write", "path": "USER.md", "content": "x"}}},
			want:  true,
		},
		{
			name:  "write to OPS.md detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "write", "path": "OPS.md", "content": "x"}}},
			want:  true,
		},
		{
			name:  "write to AGENT.md detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "write", "path": "AGENT.md", "content": "x"}}},
			want:  true,
		},
		{
			name:  "path with directory prefix detected by basename",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "write", "path": "subdir/SOUL.md", "content": "x"}}},
			want:  true,
		},
		{
			name:  "read action not detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "read", "path": "TOOLS.md"}}},
			want:  false,
		},
		{
			name:  "list action not detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "list"}}},
			want:  false,
		},
		{
			name:  "non-overlay file write not detected",
			calls: []llm.ToolCall{{Name: "file", Arguments: map[string]any{"action": "write", "path": "notes.md", "content": "x"}}},
			want:  false,
		},
		{
			name:  "other tool not detected",
			calls: []llm.ToolCall{{Name: "web_search", Arguments: map[string]any{"query": "TOOLS.md"}}},
			want:  false,
		},
		{
			name:  "nil calls returns false",
			calls: nil,
			want:  false,
		},
		{
			name:  "empty calls returns false",
			calls: []llm.ToolCall{},
			want:  false,
		},
		{
			name: "non-overlay before overlay — detected",
			calls: []llm.ToolCall{
				{Name: "file", Arguments: map[string]any{"action": "write", "path": "notes.md", "content": "x"}},
				{Name: "file", Arguments: map[string]any{"action": "write", "path": "SOUL.md", "content": "x"}},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := overlayWriteInCalls(tc.calls)
			if got != tc.want {
				t.Errorf("overlayWriteInCalls() = %v, want %v", got, tc.want)
			}
		})
	}
}
