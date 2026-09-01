package conversations

import (
	"strings"
	"testing"
)

func TestNormalizeHistoryPageSize(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{
			name:  "below minimum",
			value: 0,
			want:  defaultHistoryPageTurns,
		},
		{
			name:  "at minimum",
			value: minHistoryPageTurns,
			want:  minHistoryPageTurns,
		},
		{
			name:  "in range",
			value: 100,
			want:  100,
		},
		{
			name:  "at maximum",
			value: maxHistoryPageTurns,
			want:  maxHistoryPageTurns,
		},
		{
			name:  "above maximum",
			value: maxHistoryPageTurns + 1,
			want:  defaultHistoryPageTurns,
		},
		{
			name:  "negative",
			value: -10,
			want:  defaultHistoryPageTurns,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeHistoryPageSize(tt.value); got != tt.want {
				t.Errorf("normalizeHistoryPageSize(%d) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsAlwaysBlock(t *testing.T) {
	tests := []struct {
		name string
		turn Turn
		want bool
	}{
		{
			name: "always block",
			turn: Turn{Role: "user", ToolCallID: alwaysBlockMarker},
			want: true,
		},
		{
			name: "always block but wrong role",
			turn: Turn{Role: "assistant", ToolCallID: alwaysBlockMarker},
			want: false,
		},
		{
			name: "not always block",
			turn: Turn{Role: "user", ToolCallID: "some-other-id"},
			want: false,
		},
		{
			name: "empty tool call id",
			turn: Turn{Role: "user", ToolCallID: ""},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAlwaysBlock(tt.turn); got != tt.want {
				t.Errorf("isAlwaysBlock(%+v) = %v, want %v", tt.turn, got, tt.want)
			}
		})
	}
}

func TestReadToolOutputSpillID(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		fallback string
		want     string
	}{
		{
			name:     "with spill footer and tool call id",
			content:  "output\n[output truncated: ...] read_tool_output(tool_call_id=\"spill-123\")",
			fallback: "fallback",
			want:     "spill-123",
		},
		{
			name:     "without spill footer",
			content:  "plain output",
			fallback: "fallback",
			want:     "fallback",
		},
		{
			name:     "empty content",
			content:  "",
			fallback: "fallback",
			want:     "fallback",
		},
		{
			name:     "retained footer with tool call id",
			content:  "output\n[full output also retained: ...] read_tool_output(tool_call_id=\"retained-456\")",
			fallback: "fallback",
			want:     "retained-456",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readToolOutputSpillID(tt.content, tt.fallback); got != tt.want {
				t.Errorf("readToolOutputSpillID(%q, %q) = %q, want %q", tt.content, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestIsSidecarBacked(t *testing.T) {
	tests := []struct {
		name string
		turn Turn
		want bool
	}{
		{
			name: "sidecar backed by path",
			turn: Turn{ContentSidecarPath: "/path/to/sidecar"},
			want: true,
		},
		{
			name: "sidecar backed by spill footer",
			turn: Turn{Content: "output [output truncated: ...]"},
			want: true,
		},
		{
			name: "sidecar backed by retained footer",
			turn: Turn{Content: "output [full output also retained: ...]"},
			want: true,
		},
		{
			name: "not sidecar backed",
			turn: Turn{ToolCallID: "other-tool", Content: "plain output"},
			want: false,
		},
		{
			name: "empty turn",
			turn: Turn{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSidecarBacked(tt.turn); got != tt.want {
				t.Errorf("isSidecarBacked(%+v) = %v, want %v", tt.turn, got, tt.want)
			}
		})
	}
}

func TestToolSearchResultIDs(t *testing.T) {
	tests := []struct {
		name  string
		turns []Turn
		want  map[string]struct{}
	}{
		{
			name:  "empty turns",
			turns: []Turn{},
			want:  map[string]struct{}{},
		},
		{
			name: "single search turn",
			turns: []Turn{
				{ToolCalls: []byte(`[{"id": "result1", "function": {"name": "` + toolSearchName + `"}}]`)},
				{ToolCalls: []byte(`[{"id": "other", "function": {"name": "other_tool"}}]`)},
			},
			want: map[string]struct{}{"result1": {}},
		},
		{
			name: "multiple search turns",
			turns: []Turn{
				{ToolCalls: []byte(`[{"id": "result1", "function": {"name": "` + toolSearchName + `"}}]`)},
				{ToolCalls: []byte(`[{"id": "result2", "function": {"name": "` + toolSearchName + `"}}]`)},
				{ToolCalls: []byte(`[{"id": "other", "function": {"name": "other_tool"}}]`)},
			},
			want: map[string]struct{}{"result1": {}, "result2": {}},
		},
		{
			name: "no tool calls",
			turns: []Turn{
				{ToolCallID: toolSearchName},
			},
			want: map[string]struct{}{},
		},
		{
			name: "invalid JSON in tool calls",
			turns: []Turn{
				{ToolCalls: []byte("{invalid json")},
			},
			want: map[string]struct{}{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolSearchResultIDs(tt.turns)
			if len(got) != len(tt.want) {
				t.Errorf("toolSearchResultIDs() returned %d items, want %d", len(got), len(tt.want))
				return
			}
			for k := range tt.want {
				if _, exists := got[k]; !exists {
					t.Errorf("toolSearchResultIDs() missing key %q", k)
				}
			}
		})
	}
}

func TestInjectAlwaysBlock(t *testing.T) {
	blockContent := "always block content"
	// Test with system turn at seq=1
	turns := []Turn{
		{Seq: 1, Role: "system", Content: "system prompt"},
		{Seq: 3, Role: "user", Content: "user message"},
	}
	got := injectAlwaysBlock(turns, blockContent)
	if len(got) != 3 {
		t.Errorf("injectAlwaysBlock() returned %d turns, want 3", len(got))
		return
	}
	// Always block should be inserted at position 1 (after system turn)
	if got[1].Content != blockContent {
		t.Errorf("always block content = %q, want %q", got[1].Content, blockContent)
	}
	if got[1].Seq != alwaysBlockSeq {
		t.Errorf("always block seq = %d, want %d", got[1].Seq, alwaysBlockSeq)
	}
	if got[1].ToolCallID != alwaysBlockMarker {
		t.Errorf("always block marker = %q, want %q", got[1].ToolCallID, alwaysBlockMarker)
	}
	if got[1].Role != "user" {
		t.Errorf("always block role = %q, want %q", got[1].Role, "user")
	}
	// Test with empty block
	gotEmpty := injectAlwaysBlock(turns, "")
	if len(gotEmpty) != len(turns) {
		t.Errorf("injectAlwaysBlock with empty block changed length: got %d, want %d", len(gotEmpty), len(turns))
	}
}

func TestSplitHeadHistoryActive(t *testing.T) {
	tests := []struct {
		name           string
		turns          []Turn
		wantHeadLen    int
		wantHistoryLen int
		wantActiveLen  int
	}{
		{
			name:           "empty",
			turns:          []Turn{},
			wantHeadLen:    0,
			wantHistoryLen: 0,
			wantActiveLen:  0,
		},
		{
			name: "single user turn",
			turns: []Turn{
				{Seq: 1, Role: "user", Content: "msg"},
			},
			wantHeadLen:    0,
			wantHistoryLen: 0,
			wantActiveLen:  1,
		},
		{
			name: "assistant then user",
			turns: []Turn{
				{Seq: 1, Role: "assistant", Content: "reply"},
				{Seq: 2, Role: "user", Content: "msg"},
			},
			wantHeadLen:    0,
			wantHistoryLen: 1,
			wantActiveLen:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head, history, active := splitHeadHistoryActive(tt.turns)
			if len(head) != tt.wantHeadLen {
				t.Errorf("head length = %d, want %d", len(head), tt.wantHeadLen)
			}
			if len(history) != tt.wantHistoryLen {
				t.Errorf("history length = %d, want %d", len(history), tt.wantHistoryLen)
			}
			if len(active) != tt.wantActiveLen {
				t.Errorf("active length = %d, want %d", len(active), tt.wantActiveLen)
			}
		})
	}
}

func TestRetainedFooterMarker(t *testing.T) {
	// Test that retainedFooterMarker constant is as expected
	if !strings.Contains(retainedFooterMarker, "[full output also retained:") {
		t.Errorf("retainedFooterMarker = %q, should contain expected text", retainedFooterMarker)
	}
}

func TestSpillFooterMarker(t *testing.T) {
	// Test that spillFooterMarker constant is as expected
	if !strings.Contains(spillFooterMarker, "[output truncated:") {
		t.Errorf("spillFooterMarker = %q, should contain expected text", spillFooterMarker)
	}
}

func TestRuneStart(t *testing.T) {
	tests := []struct {
		name string
		s    string
		i    int
		want int
	}{
		{
			name: "empty string",
			s:    "",
			i:    0,
			want: 0,
		},
		{
			name: "start of string",
			s:    "hello",
			i:    0,
			want: 0,
		},
		{
			name: "middle of ascii",
			s:    "hello",
			i:    2,
			want: 2,
		},
		{
			name: "end of string",
			s:    "hello",
			i:    5,
			want: 5,
		},
		{
			name: "beyond end",
			s:    "hello",
			i:    10,
			want: 5,
		},
		{
			name: "negative",
			s:    "hello",
			i:    -1,
			want: 0,
		},
		{
			name: "utf8 continuation byte",
			s:    "a日本c", // 日本 is 2 bytes each in UTF-8
			i:    2,      // points to the continuation byte of 日
			want: 1,      // should move back to the start of 日
		},
		{
			name: "utf8 start byte",
			s:    "a日本c",
			i:    1, // points to the start of 日
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runeStart(tt.s, tt.i); got != tt.want {
				t.Errorf("runeStart(%q, %d) = %d, want %d", tt.s, tt.i, got, tt.want)
			}
		})
	}
}

func TestEvictedResultToolName(t *testing.T) {
	tests := []struct {
		name  string
		turn  Turn
		names map[string]string
		want  string
	}{
		{
			name:  "from names map",
			turn:  Turn{ToolCallID: "call1"},
			names: map[string]string{"call1": "tool_name"},
			want:  "tool_name",
		},
		{
			name:  "from content",
			turn:  Turn{ToolCallID: "unknown", Content: "[tool_name] result"},
			names: map[string]string{},
			want:  "tool_name",
		},
		{
			name:  "from content with spaces in name",
			turn:  Turn{Content: "[tool name] result"},
			names: map[string]string{},
			want:  "", // spaces are not allowed
		},
		{
			name:  "from content with newline in name",
			turn:  Turn{Content: "[tool\nname] result"},
			names: map[string]string{},
			want:  "", // newlines are not allowed
		},
		{
			name:  "empty content",
			turn:  Turn{Content: ""},
			names: map[string]string{},
			want:  "",
		},
		{
			name:  "no brackets in content",
			turn:  Turn{Content: "result"},
			names: map[string]string{},
			want:  "",
		},
		{
			name:  "names map takes precedence",
			turn:  Turn{ToolCallID: "call1", Content: "[other] result"},
			names: map[string]string{"call1": "tool_name"},
			want:  "tool_name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evictedResultToolName(tt.turn, tt.names); got != tt.want {
				t.Errorf("evictedResultToolName(%+v, %v) = %q, want %q", tt.turn, tt.names, got, tt.want)
			}
		})
	}
}

func TestIsPlainUserTurn(t *testing.T) {
	tests := []struct {
		name string
		turn Turn
		want bool
	}{
		{
			name: "plain user turn",
			turn: Turn{Role: "user", Content: "hello"},
			want: true,
		},
		{
			name: "assistant turn",
			turn: Turn{Role: "assistant", Content: "hello"},
			want: false,
		},
		{
			name: "system turn",
			turn: Turn{Role: "system", Content: "hello"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlainUserTurn(tt.turn); got != tt.want {
				t.Errorf("isPlainUserTurn(%+v) = %v, want %v", tt.turn, got, tt.want)
			}
		})
	}
}

func TestDropRepeatedUserTurns(t *testing.T) {
	tests := []struct {
		name  string
		turns []Turn
		want  []Turn
	}{
		{
			name:  "empty",
			turns: []Turn{},
			want:  []Turn{},
		},
		{
			name:  "single turn",
			turns: []Turn{{Role: "user", Content: "hello"}},
			want:  []Turn{{Role: "user", Content: "hello"}},
		},
		{
			name: "no repeats",
			turns: []Turn{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "world"},
			},
			want: []Turn{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "world"},
			},
		},
		{
			name: "consecutive user repeats",
			turns: []Turn{
				{Role: "user", Content: "hello"},
				{Role: "user", Content: "hello"},
			},
			want: []Turn{
				{Role: "user", Content: "hello"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dropRepeatedUserTurns(tt.turns)
			if len(got) != len(tt.want) {
				t.Errorf("dropRepeatedUserTurns() returned %d turns, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i].Content != tt.want[i].Content {
					t.Errorf("turn[%d].Content = %q, want %q", i, got[i].Content, tt.want[i].Content)
				}
				if got[i].Role != tt.want[i].Role {
					t.Errorf("turn[%d].Role = %q, want %q", i, got[i].Role, tt.want[i].Role)
				}
			}
		})
	}
}
