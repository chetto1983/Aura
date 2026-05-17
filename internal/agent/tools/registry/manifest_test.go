package tools

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestRenderToolManifest_EmptyReturnsEmpty(t *testing.T) {
	if got := RenderToolManifest(nil); got != "" {
		t.Fatalf("nil defs: got %q", got)
	}
	if got := RenderToolManifest([]llm.ToolDefinition{}); got != "" {
		t.Fatalf("empty defs: got %q", got)
	}
}

func TestRenderToolManifest_SortsAlphabeticallyForCacheStability(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Name: "web", Description: "Search the web or fetch a URL"},
		{Name: "create_docx", Description: "Generate a Word document"},
		{Name: "mcp_mail_imap_search_messages", Description: "Search IMAP messages"},
	}
	got := RenderToolManifest(defs)

	// Find positions of each tool name
	posDocx := strings.Index(got, "- create_docx")
	posMail := strings.Index(got, "- mcp_mail_imap_search_messages")
	posWeb := strings.Index(got, "- web")
	if posDocx < 0 || posMail < 0 || posWeb < 0 {
		t.Fatalf("missing tool in manifest:\n%s", got)
	}
	if posDocx >= posMail || posMail >= posWeb {
		t.Fatalf("not alphabetical: docx=%d mail=%d web=%d\n%s", posDocx, posMail, posWeb, got)
	}
}

func TestRenderToolManifest_TruncatesLongDescriptions(t *testing.T) {
	long := strings.Repeat("verylong ", 30) // 270 chars
	defs := []llm.ToolDefinition{
		{Name: "bloated_tool", Description: long},
	}
	got := RenderToolManifest(defs)
	line := ""
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "- bloated_tool") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("bloated_tool line missing:\n%s", got)
	}
	if len(line) > 110 {
		t.Fatalf("line too long (%d chars): %q", len(line), line)
	}
	if !strings.HasSuffix(line, "…") {
		t.Fatalf("expected ellipsis on truncated line: %q", line)
	}
}

func TestRenderToolManifest_KeepsFirstSentence(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Name: "create_docx", Description: "Generate a Word document. This is the second sentence that should be dropped."},
	}
	got := RenderToolManifest(defs)
	if !strings.Contains(got, "Generate a Word document") {
		t.Fatalf("first sentence missing:\n%s", got)
	}
	if strings.Contains(got, "second sentence") {
		t.Fatalf("second sentence leaked into manifest:\n%s", got)
	}
}

func TestRenderToolManifest_IncludesUsageHint(t *testing.T) {
	defs := []llm.ToolDefinition{{Name: "x", Description: "y"}}
	got := RenderToolManifest(defs)
	if !strings.Contains(got, "tool_search") {
		t.Fatalf("manifest must mention tool_search:\n%s", got)
	}
}

func TestRenderToolManifest_SizeBudgetFor68Tools(t *testing.T) {
	// Synthesize 68 tools mirroring real Aura catalog size (built-in + MCP).
	defs := make([]llm.ToolDefinition, 0, 68)
	for i := 0; i < 68; i++ {
		defs = append(defs, llm.ToolDefinition{
			Name:        "tool_" + string(rune('a'+(i%26))) + string(rune('a'+((i/26)%26))) + "_" + string(rune('0'+(i%10))),
			Description: "Synthetic tool number " + string(rune('A'+(i%26))) + " that does a representative job for benchmarking the manifest size budget.",
		})
	}
	got := RenderToolManifest(defs)
	chars := len(got)
	if chars > 7000 {
		t.Fatalf("manifest too large for 68 tools: %d chars (budget ~5000-7000)", chars)
	}
	if chars < 2000 {
		t.Fatalf("manifest suspiciously small: %d chars (expected ~4000-7000)", chars)
	}
}
