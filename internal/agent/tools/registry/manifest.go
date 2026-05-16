package tools

import (
	"sort"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// manifestDescLimit caps how much of each tool description goes into the
// system-prompt manifest. The manifest lists every registered tool by
// name + one-line description; full input schemas are deferred and pulled
// via tool_search. Chosen to keep the whole block ~1000-1500 tokens even
// when MCP servers add 30+ tools.
const manifestDescLimit = 70

// RenderToolManifest returns a compact 1-line-per-tool catalog block for
// the system prompt. Format:
//
//	## Tool Catalog
//	Every registered tool is listed below by name + short description. Full
//	input schemas are NOT loaded — call tool_search(query) to fetch them
//	for the tools you want to invoke, OR call a tool directly by name and
//	the agent loop will permissive-load its schema for this turn.
//
//	- create_docx — Generate a Word document (.docx) from blocks
//	- create_xlsx — Generate an Excel workbook (.xlsx) from rows
//	- ...
//
// Sorted alphabetically for cache stability (the prompt-cache prefix
// changes only when the registry membership actually changes).
//
// Returns an empty string when there are no definitions so the caller
// can omit the block.
func RenderToolManifest(defs []llm.ToolDefinition) string {
	if len(defs) == 0 {
		return ""
	}
	sorted := append([]llm.ToolDefinition(nil), defs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	b.WriteString("## Tool Catalog\n")
	b.WriteString("Every registered tool is listed below by name and short description. Full input schemas are NOT loaded in this turn's pool — call tool_search(query) to fetch them, OR invoke a tool directly by name (the agent loop will load its schema for this turn).\n\n")
	for _, d := range sorted {
		b.WriteString("- ")
		b.WriteString(d.Name)
		desc := summarizeForManifest(d.Description)
		if desc != "" {
			b.WriteString(" — ")
			b.WriteString(desc)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// summarizeForManifest collapses a tool description to a single short
// sentence suitable for the catalog manifest. Strips trailing whitespace,
// keeps only the first sentence, and caps at manifestDescLimit chars with
// an ellipsis when needed.
func summarizeForManifest(desc string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}
	// Take the first sentence — descriptions in this codebase usually
	// have a leading "Do X." then elaboration; the leading X is the
	// useful signal.
	if idx := strings.IndexAny(desc, ".!?\n"); idx > 0 && idx < manifestDescLimit*2 {
		desc = desc[:idx]
	}
	desc = strings.TrimSpace(desc)
	// Collapse internal whitespace.
	desc = strings.Join(strings.Fields(desc), " ")
	if len(desc) > manifestDescLimit {
		// Cap on a word boundary when possible.
		cut := manifestDescLimit
		if sp := strings.LastIndexByte(desc[:manifestDescLimit], ' '); sp > manifestDescLimit/2 {
			cut = sp
		}
		desc = strings.TrimSpace(desc[:cut]) + "…"
	}
	return desc
}

// ManifestTokenEstimate returns a rough char count of the manifest so
// callers can decide whether to truncate or split. The actual token count
// depends on the tokenizer but is roughly chars/4.
func ManifestTokenEstimate(manifest string) int {
	if manifest == "" {
		return 0
	}
	return len(manifest) / 4
}
