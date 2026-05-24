package tools

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// makeSplitDef is a test helper that builds a ToolDefinition with the given
// name, description, and visibility tier for RenderSplitManifest probes.
func makeSplitDef(name, desc string, tier VisibilityTier) ToolDefinition {
	return ToolDefinition{Name: name, Description: desc, VisibilityTier: tier}
}

// TestRenderSplitManifest_EmptyReturnsEmpty mirrors the RenderToolManifest nil/empty check.
func TestRenderSplitManifest_EmptyReturnsEmpty(t *testing.T) {
	if got := RenderSplitManifest(nil); got != "" {
		t.Fatalf("nil defs: got %q", got)
	}
	if got := RenderSplitManifest([]ToolDefinition{}); got != "" {
		t.Fatalf("empty defs: got %q", got)
	}
}

// TestRenderSplitManifest_ZeroDeferredByteIdentical verifies AC #4:
// when no tools are deferred, output is byte-identical to RenderToolManifest.
//
// Tools are built without examples so renderToolDescription adds nothing and
// the llm.ToolDefinition.Description == the plain ToolDefinition.Description.
func TestRenderSplitManifest_ZeroDeferredByteIdentical(t *testing.T) {
	names := []string{
		"web", "create_docx", "search_memory", "schedule_task",
		"file", "wiki", "execute_code", "source", "ask_user",
		"text_response", "doc", "mcp_mail_search", "workspace_read",
		"workspace_write", "ingest", "read_source", "store_source",
		"delete_source", "list_source", "ocr_source", "search",
		"recall_operational", "recall_user_memory", "agent_note",
	}
	// 25 tools, all VisibilityActiveTurn (non-deferred).
	splitDefs := make([]ToolDefinition, len(names))
	llmDefs := make([]llm.ToolDefinition, len(names))
	for i, n := range names {
		desc := "Synthetic description for " + n + " tool. Second sentence excluded."
		splitDefs[i] = makeSplitDef(n, desc, VisibilityActiveTurn)
		llmDefs[i] = llm.ToolDefinition{Name: n, Description: desc}
	}

	gotSplit := RenderSplitManifest(splitDefs)
	gotOld := RenderToolManifest(llmDefs)

	if gotSplit != gotOld {
		t.Fatalf("RenderSplitManifest with 0 deferred tools is not byte-identical to RenderToolManifest\n"+
			"--- RenderSplitManifest ---\n%s\n--- RenderToolManifest ---\n%s", gotSplit, gotOld)
	}
}

// TestRenderSplitManifest_DeferredSectionAppearsAndActiveSectionExcludesDeferred verifies AC #5:
// deferred section appears when tools have VisibilityDeferred, and deferred tools
// do NOT appear in the always-on (## Tool Catalog) section.
func TestRenderSplitManifest_DeferredSectionAppearsAndActiveSectionExcludesDeferred(t *testing.T) {
	active := make([]ToolDefinition, 23)
	for i := range active {
		active[i] = makeSplitDef(
			"active_tool_"+string(rune('a'+i)),
			"Active tool description.",
			VisibilityActiveTurn,
		)
	}
	deferred := []ToolDefinition{
		makeSplitDef("execute_code", "Execute Python code in the sandbox.", VisibilityDeferred),
		makeSplitDef("execute_shell", "Execute shell commands in the runtime.", VisibilityDeferred),
	}
	all := append(active, deferred...)

	got := RenderSplitManifest(all)

	// Deferred section marker must be present.
	const deferredHeader = "## Deferred tools (call tool_search to load schema)"
	if !strings.Contains(got, deferredHeader) {
		t.Fatalf("deferred section marker missing:\n%s", got)
	}

	// Both deferred tool names must appear in the deferred section (after the marker).
	deferredSectionStart := strings.Index(got, deferredHeader)
	deferredSection := got[deferredSectionStart:]
	for _, name := range []string{"execute_code", "execute_shell"} {
		if !strings.Contains(deferredSection, "- "+name) {
			t.Errorf("deferred tool %q missing from deferred section:\n%s", name, deferredSection)
		}
	}

	// Deferred tools must NOT appear in the always-on (Tool Catalog) section.
	catalogSection := got[:deferredSectionStart]
	for _, name := range []string{"execute_code", "execute_shell"} {
		if strings.Contains(catalogSection, "- "+name) {
			t.Errorf("deferred tool %q leaked into always-on catalog section:\n%s", name, catalogSection)
		}
	}

	// All 23 active tools must appear in the catalog section.
	for _, d := range active {
		if !strings.Contains(catalogSection, "- "+d.Name) {
			t.Errorf("active tool %q missing from catalog section:\n%s", d.Name, catalogSection)
		}
	}
}

// TestRenderSplitManifest_AllDeferredNoAlwaysOnSection verifies that when all
// tools are deferred, the ## Tool Catalog header is NOT emitted.
func TestRenderSplitManifest_AllDeferredNoAlwaysOnSection(t *testing.T) {
	defs := []ToolDefinition{
		makeSplitDef("execute_code", "Execute Python code.", VisibilityDeferred),
		makeSplitDef("execute_shell", "Execute shell commands.", VisibilityDeferred),
	}
	got := RenderSplitManifest(defs)
	if strings.Contains(got, "## Tool Catalog") {
		t.Fatalf("## Tool Catalog appeared when all tools are deferred:\n%s", got)
	}
	if !strings.Contains(got, "## Deferred tools (call tool_search to load schema)") {
		t.Fatalf("deferred section missing:\n%s", got)
	}
}

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
