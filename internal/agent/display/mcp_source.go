package display

import (
	"encoding/json"
	"strings"
)

// TrustedMCP is a bridge-owned recipe identity attached to a completed MCP call.
// Only the host result metadata or an owner-scoped invocation fact may supply it.
type TrustedMCP struct {
	Recipe string
	Tool   string
	Action string
}

// TrustedMCPFromMeta accepts the exact metadata shape the managed bridge emits.
// Matching the model-facing name and calendar arguments stops a marker for one
// call from being reused on another call during snapshot reconstruction.
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
	switch recipe {
	case "recipe:memory", "recipe:whatsapp":
		if len(raw) != 2 {
			return nil
		}
	case "recipe:calendar":
		if tool != "calendar" || len(raw) != 3 {
			return nil
		}
		action, actionOK := raw["action"].(string)
		if !actionOK || action == "" {
			return nil
		}
		var args struct {
			Action string `json:"action"`
		}
		if json.Unmarshal([]byte(arguments), &args) != nil || args.Action != action {
			return nil
		}
		marker.Action = action
	default:
		return nil
	}
	return marker
}
