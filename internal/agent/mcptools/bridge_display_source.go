package mcptools

// displaySource records the bridge's host-owned trust decision after a successful
// MCP call: the recipe the mount was trusted as and the raw tool it called. Model
// arguments and server output cannot supply this marker.
func displaySource(source, tool string) (map[string]any, bool) {
	actions := trustedRecipeActions[source]
	if actions == nil || actions[tool] == mcpActionUnknown {
		return nil, false
	}
	return map[string]any{"recipe": source, "tool": tool}, true
}
