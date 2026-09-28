package display

import (
	"strings"
	"testing"
)

func TestSearchFilesPreview(t *testing.T) {
	for _, tc := range []struct {
		name, args, result string
		columns            []string
		rows               int
		first              []string
		notice             bool
	}{
		{"content", `{"pattern":"needle","target":"content","output_mode":"content"}`, "src/a.go:12: needle <b>", []string{"File", "Line", "Kind", "Text"}, 1, []string{"src/a.go", "12", "match", "needle <b>"}, false},
		{"path colon", `{"pattern":"needle"}`, "src:one/a.go:12: needle", []string{"File", "Line", "Kind", "Text"}, 1, []string{"src:one/a.go", "12", "match", "needle"}, false},
		{"context", `{"pattern":"needle","context":1}`, "src/a.go-11- before\nsrc/a.go:12: needle", []string{"File", "Line", "Kind", "Text"}, 2, []string{"src/a.go", "11", "context", "before"}, false},
		{"files only", `{"pattern":"needle","output_mode":"files_only"}`, "src/a.go\nsrc/b.go", []string{"File"}, 2, []string{"src/a.go"}, false},
		{"file target", `{"pattern":"*.go","target":"files"}`, "src/a.go", []string{"File"}, 1, []string{"src/a.go"}, false},
		{"count", `{"pattern":"needle","output_mode":"count"}`, "src/a.go: 3", []string{"File", "Count"}, 1, []string{"src/a.go", "3"}, false},
		{"empty", `{"pattern":"needle"}`, "[no matches]", []string{"File", "Line", "Kind", "Text"}, 0, nil, false},
		{"walk truncated", `{"pattern":"needle"}`, "src/a.go:12: needle\n[walk truncated: hit the node/time budget — results are partial, not exhaustive; narrow `path` or `glob`]", []string{"File", "Line", "Kind", "Text"}, 1, []string{"src/a.go", "12", "match", "needle"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "q1", ToolName: "search_files", Arguments: tc.args, ResultPreview: tc.result}, NewRegistry())
			if !ok || p.Table == nil || len(p.Table.Rows) != tc.rows || len(p.Table.Columns) != len(tc.columns) || (p.Table.Notice != "") != tc.notice {
				t.Fatalf("search table = %+v, ok=%v", p, ok)
			}
			for i, want := range tc.first {
				if p.Table.Rows[0][i] != want {
					t.Fatalf("cell %d = %q, want %q", i, p.Table.Rows[0][i], want)
				}
			}
		})
	}
}

func TestSearchFilesFallback(t *testing.T) {
	args := `{"pattern":"needle"}`
	for _, tc := range []struct{ name, args, result string }{
		{"missing pattern", `{}`, "a:1: needle"},
		{"ambiguous delimiter", args, "src/a:12: text:34: needle"},
		{"bad line", args, "src/a:line: needle"},
		{"error", args, "search_files: invalid pattern"},
		{"huge", args, strings.Repeat("x", 65537)},
		{"unknown mode", `{"pattern":"needle","output_mode":"surprise"}`, "a:1: needle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "q2", ToolName: "search_files", Arguments: tc.args, ResultPreview: tc.result}, NewRegistry()); ok {
				t.Fatalf("unexpected rich search = %+v", p)
			}
		})
	}
}

func TestNativeToolDisposition(t *testing.T) {
	for _, name := range []string{"shell_kill", "read_tool_output", "tool_search", "current_time", "text_response"} {
		if p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "other", ToolName: name, Arguments: `{}`, ResultPreview: "plain result"}, NewRegistry()); ok {
			t.Errorf("%s unexpectedly promoted: %+v", name, p)
		}
	}
	p, ok := NormalizeToolPreview(PreviewInput{
		ToolCallID: "sandbox", ToolName: "sandbox_exec", Arguments: `{"command":"echo ready"}`,
		ResultPreview: "ready\n[aura_shell {\"exit_code\":0,\"cwd\":\"/workspace\",\"duration_ms\":3}]",
	}, NewRegistry())
	if !ok || p.Type != KindTerminal || p.Terminal == nil || p.Terminal.Command != "echo ready" {
		t.Fatalf("completed sandbox shell = %+v, %v", p, ok)
	}
}
