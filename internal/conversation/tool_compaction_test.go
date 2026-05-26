package conversation

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestCompactCompletedToolResultsPreservesProtocol(t *testing.T) {
	ctx := NewContext(Config{})
	ctx.AddUserMessage("run check")
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: "call_1", Name: "execute_shell"}})
	ctx.AddToolResultMessage("call_1", strings.Repeat("line\n", 5000))
	ctx.AddAssistantMessage("done")

	changed := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{
		MaxChars:       400,
		KeepRecentFull: 0,
	})
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	msgs := ctx.Messages()
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_1" {
		t.Fatalf("tool protocol changed: %#v", msgs[2])
	}
	if len(msgs[2].Content) > 500 {
		t.Fatalf("compacted content too long: %d", len(msgs[2].Content))
	}
	for _, want := range []string{"[tool result compacted]", "tool: execute_shell", "original_chars:"} {
		if !strings.Contains(msgs[2].Content, want) {
			t.Fatalf("compacted content = %q, missing %q", msgs[2].Content, want)
		}
	}
}

func TestCompactCompletedToolResultsWaitsForLaterAssistantMessage(t *testing.T) {
	ctx := NewContext(Config{})
	ctx.AddUserMessage("run check")
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: "call_1", Name: "execute_shell"}})
	ctx.AddToolResultMessage("call_1", strings.Repeat("line\n", 5000))

	changed := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{
		MaxChars: 120,
	})
	if changed != 0 {
		t.Fatalf("changed = %d, want 0", changed)
	}
	if got := ctx.Messages()[2].Content; !strings.Contains(got, "line") {
		t.Fatalf("tool result changed before assistant finalization: %q", got)
	}
}

func TestCompactCompletedToolResultsKeepsRecentFullResults(t *testing.T) {
	ctx := NewContext(Config{})
	for _, id := range []string{"call_1", "call_2", "call_3"} {
		ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: id, Name: "read_file"}})
		ctx.AddToolResultMessage(id, strings.Repeat(id+"\n", 200))
		ctx.AddAssistantMessage("done " + id)
	}

	changed := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{
		MaxChars:       120,
		KeepRecentFull: 1,
	})
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	msgs := ctx.Messages()
	if !strings.Contains(msgs[1].Content, "[tool result compacted]") {
		t.Fatalf("oldest result was not compacted: %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[4].Content, "[tool result compacted]") {
		t.Fatalf("middle result was not compacted: %q", msgs[4].Content)
	}
	if strings.Contains(msgs[7].Content, "[tool result compacted]") {
		t.Fatalf("newest result should stay full: %q", msgs[7].Content)
	}
}

func TestCompactCompletedToolResultsRedactsSecrets(t *testing.T) {
	ctx := NewContext(Config{})
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: "call_1", Name: "execute_shell"}})
	ctx.AddToolResultMessage("call_1", strings.Join([]string{
		"Authorization: Bearer secret-token",
		"password=my-password",
		"normal line",
	}, "\n"))
	ctx.AddAssistantMessage("done")

	changed := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{
		MaxChars:       200,
		KeepRecentFull: 0,
	})
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	got := ctx.Messages()[1].Content
	for _, leaked := range []string{"secret-token", "my-password", "Authorization: Bearer"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("compacted result leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "normal line") {
		t.Fatalf("compacted result dropped safe preview: %q", got)
	}
}

func TestCompactToolResultContentKeepsHeadAndTailFacts(t *testing.T) {
	content := strings.Join([]string{
		"HEAD_IMPORTANT: first failure is package alpha",
		strings.Repeat("middle noise line with stack frame\n", 80),
		"TAIL_IMPORTANT: final exit code 42 and remediation hint",
	}, "\n")

	got := CompactToolResultContent("execute_shell", content, 420)
	for _, want := range []string{
		"[tool result compacted]",
		"tool: execute_shell",
		"HEAD_IMPORTANT",
		"TAIL_IMPORTANT",
		"bytes omitted from tool preview",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compacted content missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, strings.Repeat("middle noise line with stack frame\n", 20)) {
		t.Fatalf("compacted content kept too much middle noise:\n%s", got)
	}
}

func TestCompactCompletedToolResultsIsIdempotent(t *testing.T) {
	ctx := NewContext(Config{})
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: "call_1", Name: "execute_shell"}})
	ctx.AddToolResultMessage("call_1", strings.Repeat("line\n", 5000))
	ctx.AddAssistantMessage("done")

	first := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{MaxChars: 120})
	second := ctx.CompactCompletedToolResults(ToolResultCompactionPolicy{MaxChars: 120})
	if first != 1 || second != 0 {
		t.Fatalf("compaction counts = (%d, %d), want (1, 0)", first, second)
	}
}
