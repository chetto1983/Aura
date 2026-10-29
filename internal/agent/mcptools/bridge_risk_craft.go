package mcptools

import "github.com/chetto1983/aura/internal/mcp"

// craftRecipeActions classifies every tool a Craft server offers in the box (its desktop-only tools are
// never bridged: bridge_craft.go). Reads only read or render; every other tool writes, and only inside the
// identity's box, which is a reversible mutation: none is destructive. For these sources the table is the
// whole policy: a server's destructiveHint does not raise it (FilmCraft, PdfCraft and PhotoCraft declare
// doc_save, command_run and even jobs_list destructive: spikes/craft-mcp/FINDINGS.md, verdict 9), and a tool
// the table does not name fails closed instead of trusting its annotations.
var craftRecipeActions = map[string]map[string]MCPActionClass{
	mcp.CraftRecipeSource("gridcraft"): craftActions(
		[]string{"list_commands", "inspect_workbook", "read_range", "get_cell", "evaluate_formula", "list_functions",
			"list_windows"},
		[]string{"execute_command", "write_range", "set_cell", "select", "format_range", "insert_chart", "create_table",
			"sort_range", "filter", "open_workbook", "save_workbook", "new_workbook", "undo", "redo"}),
	mcp.CraftRecipeSource("wordcraft"): craftActions(
		[]string{"list_commands", "get_text", "inspect_document", "render_page", "parity", "list_documents"},
		[]string{"execute", "batch", "new_document", "open_document", "save_document", "type_text", "select_text"}),
	mcp.CraftRecipeSource("deckcraft"): craftActions(
		[]string{"list_commands", "inspect_document", "inspect_slide", "render_slide"},
		[]string{"run_command", "batch", "new_presentation", "open_presentation", "save_presentation", "export",
			"add_slide", "go_to_slide", "add_shape", "add_text_box", "set_text", "insert_picture", "insert_table",
			"insert_chart", "select", "pointer", "select_tool", "key", "type_text"}),
	mcp.CraftRecipeSource("filmcraft"): craftActions(
		[]string{"command_list", "doc_inspect", "project_inspect", "render_frame", "render_preview", "sequence_inspect"},
		[]string{"command_batch", "command_run", "media_import"}),
	mcp.CraftRecipeSource("pdfcraft"): craftActions(
		[]string{"doc_info", "page_render", "text_extract", "text_find", "command_list", "doc_inspect", "render_preview",
			"tool_search"},
		[]string{"doc_open", "doc_save", "doc_close", "doc_combine", "doc_split", "edit_undo", "command_run",
			"command_batch", "tool_call"}),
	mcp.CraftRecipeSource("photocraft"): craftActions(
		[]string{"command_list", "doc_inspect", "doc_render_preview", "render_preview", "session_list", "jobs_list"},
		[]string{"command_batch", "command_run", "doc_close", "doc_export", "doc_export_data_sets", "doc_export_layers",
			"doc_import_data_sets", "doc_new", "doc_open", "doc_place", "doc_replace_smart_object", "doc_save",
			"doc_select", "jobs_cancel"}),
}

func craftActions(reads, writes []string) map[string]MCPActionClass {
	out := make(map[string]MCPActionClass, len(reads)+len(writes))
	for _, name := range reads {
		out[name] = MCPActionRead
	}
	for _, name := range writes {
		out[name] = MCPActionMutate
	}
	return out
}

// craftToolRisk is classifyToolRisk's answer for a Craft source: the table, or fail closed.
func craftToolRisk(actions map[string]MCPActionClass, tool string) (mutating, destructive bool) {
	switch actions[tool] {
	case MCPActionRead:
		return false, false
	case MCPActionMutate:
		return true, false
	default:
		return true, true
	}
}
