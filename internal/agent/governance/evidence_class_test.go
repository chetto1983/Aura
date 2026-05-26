package governance

import "testing"

func TestIsEvidenceClassTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		want     bool
	}{
		{"search all actions", "search", map[string]any{"action": "gaps"}, true},
		{"search without action", "search", nil, true},
		{"search memory legacy", "search_memory", map[string]any{"query": "aura"}, true},
		{"wiki page read", "wiki_page", map[string]any{"action": "read"}, true},
		{"wiki page write wrapper", "wiki_page", map[string]any{"action": "write"}, true},
		{"file read", "file", map[string]any{"action": "read", "path": "AGENT.md"}, true},
		{"file write", "file", map[string]any{"action": "write", "path": "AGENT.md"}, true},
		{"source read", "source", map[string]any{"action": "read"}, true},
		{"read source legacy", "read_source", map[string]any{"action": "read"}, true},
		{"source list", "source", map[string]any{"action": "list"}, false},
		{"task list", "task", map[string]any{"action": "list"}, true},
		{"task schedule", "task", map[string]any{"action": "schedule"}, false},
		{"execute shell control", "execute_shell", nil, false},
		{"web summarizer eligible", "web", map[string]any{"action": "fetch"}, false},
		{"document summarizer eligible", "create_document", map[string]any{"format": "pdf"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsEvidenceClassTool(tc.toolName, tc.args); got != tc.want {
				t.Fatalf("IsEvidenceClassTool(%q, %#v) = %v, want %v", tc.toolName, tc.args, got, tc.want)
			}
		})
	}
}
