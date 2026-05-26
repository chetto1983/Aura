package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func renderToolDescription(name, description string, examples []ToolCallExample) string {
	description = strings.TrimSpace(description)
	if len(examples) == 0 {
		return description
	}
	// Show up to 5 examples per tool. The previous cap of 2 hid action
	// variants on enum tools and caused repeated calls with missing action.
	if len(examples) > 5 {
		examples = examples[:5]
	}
	lines := make([]string, 0, len(examples))
	for _, example := range examples {
		if len(example.Arguments) == 0 {
			lines = append(lines, fmt.Sprintf("%s({})", name))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s(%s)", name, marshalExampleArgs(example.Arguments)))
	}
	return description + "\n\nTool call examples:\n- " + strings.Join(lines, "\n- ")
}

func examplesForToolName(name string, params map[string]any) []ToolCallExample {
	switch name {
	case "search":
		return []ToolCallExample{
			{Description: "Broad evidence sweep; prefer this single call over multiple narrow searches.", Arguments: map[string]any{"action": "search", "query": "wave 2.9 markitdown", "zone": "all", "top_k": 10}},
			{Description: "Wiki-only follow-up when you already have a slug to confirm.", Arguments: map[string]any{"action": "search", "query": "[[piano-di-miglioramento]]", "zone": "wiki", "top_k": 5}},
			{Description: "Approved operational lessons for a tool.", Arguments: map[string]any{"action": "lessons", "tool_name": "web", "limit": 5}},
			{Description: "Approved user facts or preferences.", Arguments: map[string]any{"action": "user_facts", "category": "preference", "limit": 5}},
			{Description: "Wiki graph neighborhood capsule.", Arguments: map[string]any{"action": "subgraph", "query": "robot calibration", "depth": 2, "budget_tokens": 1500}},
		}
	case "file":
		return []ToolCallExample{
			{Description: "List entries under a workspace directory.", Arguments: map[string]any{"action": "list", "path": "wiki", "limit": 50}},
			{Description: "Read a workspace file (UTF-8 or base64).", Arguments: map[string]any{"action": "read", "path": "wiki/index.md", "max_bytes": 4000}},
			{Description: "Substring search across text files with glob filters.", Arguments: map[string]any{"action": "search", "pattern": "Aura summary", "globs": []any{"wiki/**/*.md"}, "limit": 10}},
			{Description: "Atomic UTF-8 write.", Arguments: map[string]any{"action": "write", "path": "wiki/new-note.md", "content": "# New Note\n\nContent."}},
			{Description: "Exact text replacement inside one file.", Arguments: map[string]any{"action": "patch", "path": "wiki/note.md", "old": "old", "new": "new"}},
		}
	case "execute_code":
		return []ToolCallExample{{Arguments: map[string]any{"code": "from pathlib import Path\nPath('/tmp/aura_out/result.txt').write_text('ok')\nprint('ok')"}}}
	case "source":
		return []ToolCallExample{
			{Description: "List ingested sources.", Arguments: map[string]any{"action": "list", "status": "ingested", "limit": 10}},
			{Description: "Read the markdown body of a source.", Arguments: map[string]any{"action": "read", "source_id": "src_0123456789abcdef", "max_bytes": 4000}},
			{Description: "Store a short text note as a source.", Arguments: map[string]any{"action": "store", "kind": "text", "filename": "note.txt", "content": "Note to save as source."}},
			{Description: "Re-run the LLM ingest pipeline on an existing source.", Arguments: map[string]any{"action": "reprocess", "source_id": "src_0123456789abcdef", "stages": []any{"ingest"}}},
			{Description: "Corpus audit: broken refs, orphans, stale OCR.", Arguments: map[string]any{"action": "lint"}},
		}
	case "web":
		return []ToolCallExample{
			{Arguments: map[string]any{"action": "search", "query": "OpenAI API latest structured outputs", "max_results": 5}},
			{Arguments: map[string]any{"action": "fetch", "url": "https://example.com"}},
		}
	case "task":
		return []ToolCallExample{
			{Arguments: map[string]any{"action": "schedule", "name": "health-check", "kind": "reminder", "payload": "Run a short Aura health check.", "in": "1h"}},
			{Arguments: map[string]any{"action": "list", "status": "active"}},
			{Arguments: map[string]any{"action": "cancel", "name": "health-check"}},
			{Arguments: map[string]any{"action": "run_now", "name": "morning-watch"}},
		}
	case "install_skill":
		return []ToolCallExample{{Arguments: map[string]any{"name": "docx"}}}
	case "delete_skill":
		return []ToolCallExample{{Arguments: map[string]any{"name": "docx"}}}
	case "settings_update":
		return []ToolCallExample{{Arguments: map[string]any{"key": "AURA_AGENT_LOOP_MAX_STEPS", "value": "8"}}}
	case "read_swarm_result":
		return []ToolCallExample{{Arguments: map[string]any{"task_id": "task_123"}}}
	case "list_swarm_tasks":
		return []ToolCallExample{{Arguments: map[string]any{"run_id": "swarm_123"}}}
	default:
		return []ToolCallExample{{Arguments: exampleArgsFromSchema(params)}}
	}
}

func exampleArgsFromSchema(params map[string]any) map[string]any {
	out := map[string]any{}
	if params == nil {
		return out
	}
	properties, _ := params["properties"].(map[string]any)
	if len(properties) == 0 {
		return out
	}
	required := requiredSchemaFields(params)
	fields := required
	if len(fields) == 0 {
		fields = sortedSchemaFields(properties)
		if len(fields) > 3 {
			fields = fields[:3]
		}
	}
	for _, field := range fields {
		out[field] = exampleValueForProperty(field, properties[field])
	}
	return out
}

func requiredSchemaFields(params map[string]any) []string {
	raw, ok := params["required"].([]string)
	if ok {
		return append([]string(nil), raw...)
	}
	var out []string
	if items, ok := params["required"].([]any); ok {
		for _, item := range items {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedSchemaFields(properties map[string]any) []string {
	fields := make([]string, 0, len(properties))
	for field := range properties {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func exampleValueForProperty(field string, prop any) any {
	p, _ := prop.(map[string]any)
	if values, ok := p["enum"].([]string); ok && len(values) > 0 {
		return values[0]
	}
	if values, ok := p["enum"].([]any); ok && len(values) > 0 {
		return values[0]
	}
	switch strings.TrimSpace(fmt.Sprint(p["type"])) {
	case "integer", "number":
		return float64(1)
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return exampleStringForField(field)
	}
}

func exampleStringForField(field string) string {
	switch strings.ToLower(field) {
	case "query", "q":
		return "aura memory"
	case "path":
		return "wiki/index.md"
	case "url":
		return "https://example.com"
	case "filename":
		return "output.txt"
	case "id", "source_id", "task_id", "run_id":
		return "example_id"
	case "code":
		return "print('ok')"
	case "content":
		return "content"
	case "name":
		return "example"
	default:
		return "example"
	}
}

func marshalExampleArgs(args map[string]any) string {
	if args == nil {
		args = map[string]any{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}
