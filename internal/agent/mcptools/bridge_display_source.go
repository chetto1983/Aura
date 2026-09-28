package mcptools

// displaySource records the bridge's host-owned trust decision after a successful
// MCP call. Model arguments and server output cannot supply this marker. Calendar
// uses a multiplexed action; the other curated surfaces use raw tool names.
func (b *bridgedTool) displaySource(args map[string]any) (map[string]any, bool) {
	source := b.Spec().TrustedRecipeSource
	actions := trustedRecipeActions[source]
	if actions == nil || source == "" || source != b.policy.recipeSource {
		return nil, false
	}
	marker := map[string]any{"recipe": source, "tool": b.name}
	if source == calendarRecipeSource {
		if b.name != "calendar" {
			return nil, false
		}
		action, ok := args["action"].(string)
		if !ok || actions[action] == MCPActionUnknown {
			return nil, false
		}
		marker["action"] = action
		return marker, true
	}
	if actions[b.name] == MCPActionUnknown {
		return nil, false
	}
	return marker, true
}
