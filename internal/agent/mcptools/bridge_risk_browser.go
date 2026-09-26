package mcptools

// browserRecipeActions grades agent-browser's MCP tools (the `core` profile, measured
// 2026-09-26: 29 tools). The server marks its reads read-only and states no destructive
// hint for anything else, which the unannotated default reads as destructive: every click
// would stop the turn for approval. The same commands already run ungated through
// shell_exec in the same box, so gating them here would protect nothing.
//
// Driving a page is a reversible write to the box's browser. What a click or a script does
// on the site behind it is the site's business and is not visible here; the browser skill
// tells the agent to stop and ask before submitting anything irreversible.
var browserRecipeActions = map[string]MCPActionClass{
	"agent_browser_tools_profiles":    MCPActionRead,
	"agent_browser_read":              MCPActionRead,
	"agent_browser_snapshot":          MCPActionRead,
	"agent_browser_screenshot":        MCPActionRead,
	"agent_browser_get_text":          MCPActionRead,
	"agent_browser_get_url":           MCPActionRead,
	"agent_browser_get_title":         MCPActionRead,
	"agent_browser_wait_ms":           MCPActionRead,
	"agent_browser_wait_for_selector": MCPActionRead,
	"agent_browser_wait_for_text":     MCPActionRead,
	"agent_browser_wait_for_load":     MCPActionRead,
	"agent_browser_tab_list":          MCPActionRead,
	"agent_browser_open":              MCPActionMutate,
	"agent_browser_click":             MCPActionMutate,
	"agent_browser_fill":              MCPActionMutate,
	"agent_browser_type":              MCPActionMutate,
	"agent_browser_press":             MCPActionMutate,
	"agent_browser_check":             MCPActionMutate,
	"agent_browser_uncheck":           MCPActionMutate,
	"agent_browser_select":            MCPActionMutate,
	"agent_browser_scroll":            MCPActionMutate,
	"agent_browser_eval":              MCPActionMutate,
	"agent_browser_back":              MCPActionMutate,
	"agent_browser_forward":           MCPActionMutate,
	"agent_browser_reload":            MCPActionMutate,
	"agent_browser_tab_new":           MCPActionMutate,
	"agent_browser_tab_switch":        MCPActionMutate,
	"agent_browser_tab_close":         MCPActionMutate,
	"agent_browser_close":             MCPActionMutate,
}
