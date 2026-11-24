package display

import (
	"encoding/json"
	"strings"
)

// TrustedMCP is a bridge-owned recipe identity attached to a completed MCP call.
// Only the host result metadata or an owner-scoped invocation fact may supply it.
// Action is set only by the PIM's marker from before its split into one tool per
// operation, when every call went through the multiplexed `calendar` tool.
type TrustedMCP struct {
	Recipe string
	Tool   string
	Action string
}

// legacyCalendarTool is the PIM's one multiplexed tool from before the split.
const legacyCalendarTool = "calendar"

// TrustedMCPFromMeta accepts the exact metadata shape the managed bridge emits.
// Matching the model-facing name (and, for the old PIM marker, its action argument)
// stops a marker for one call from being reused on another during snapshot
// reconstruction.
func TrustedMCPFromMeta(meta map[string]any, toolName, arguments string) *TrustedMCP {
	raw, ok := meta["aura_display_source"].(map[string]any)
	if !ok {
		return nil
	}
	recipe, recipeOK := raw["recipe"].(string)
	tool, toolOK := raw["tool"].(string)
	if !recipeOK || !toolOK || tool == "" {
		return nil
	}
	separator := strings.LastIndex(toolName, "__")
	if separator <= 0 || toolName[separator+2:] != tool {
		return nil
	}
	marker := &TrustedMCP{Recipe: recipe, Tool: tool}
	switch {
	case len(raw) == 2 && (recipe == "recipe:memory" || recipe == "recipe:whatsapp"):
	case len(raw) == 2 && recipe == "recipe:calendar" && tool != legacyCalendarTool:
	case len(raw) == 3 && recipe == "recipe:calendar" && tool == legacyCalendarTool:
		// tool_invocations is append-only, so pre-split conversations replay this shape forever.
		action, actionOK := raw["action"].(string)
		if !actionOK || action == "" || calendarArgumentsAction(arguments) != action {
			return nil
		}
		marker.Action = action
	default:
		return nil
	}
	return marker
}

// calendarArgumentsAction reads the action a pre-split PIM call named.
func calendarArgumentsAction(arguments string) string {
	var args struct {
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return ""
	}
	return args.Action
}
