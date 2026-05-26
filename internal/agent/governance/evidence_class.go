package governance

import "strings"

// IsEvidenceClassTool reports whether a tool result carries ground-truth-dense
// identifiers, source bytes, citations, or task IDs that must not be
// paraphrased by the LLM payload summarizer.
func IsEvidenceClassTool(toolName string, args map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "file":
		return true
	case "search":
		return true
	case "search_memory":
		return true
	case "wiki_page":
		return true
	case "source", "read_source":
		return evidenceAction(args) == "read"
	case "task":
		return evidenceAction(args) == "list"
	default:
		return false
	}
}

func evidenceAction(args map[string]any) string {
	if args == nil {
		return ""
	}
	action, _ := args["action"].(string)
	return strings.ToLower(strings.TrimSpace(action))
}
