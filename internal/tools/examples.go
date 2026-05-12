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
	if len(examples) > 2 {
		examples = examples[:2]
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
	case "search_memory":
		return []ToolCallExample{{Description: "Find compact evidence across Aura memory before answering.", Arguments: map[string]any{"query": "documenti e note disponibili in Aura", "scope": "all", "limit": 3}}}
	case "list_files":
		return []ToolCallExample{{Arguments: map[string]any{"path": "wiki", "max_depth": 2}}}
	case "read_file":
		return []ToolCallExample{{Arguments: map[string]any{"path": "wiki/index.md", "max_bytes": 4000}}}
	case "search_files":
		return []ToolCallExample{{Arguments: map[string]any{"query": "Riepilogo Aura", "globs": []any{"wiki/**/*.md"}, "max_results": 10}}}
	case "write_file":
		return []ToolCallExample{{Arguments: map[string]any{"path": "wiki/nuova-nota.md", "content": "# Nuova Nota\n\nContenuto."}}}
	case "apply_patch":
		return []ToolCallExample{{Arguments: map[string]any{"path": "wiki/nota.md", "patch": "*** Begin Patch\n*** Update File: wiki/nota.md\n@@\n-vecchio\n+nuovo\n*** End Patch"}}}
	case "create_docx":
		return []ToolCallExample{{Description: "Create a Word document from typed content blocks.", Arguments: map[string]any{"filename": "riepilogo-aura.docx", "title": "Riepilogo Aura", "blocks": []any{map[string]any{"kind": "heading", "level": 1, "text": "Riepilogo Aura"}, map[string]any{"kind": "paragraph", "text": "Sintesi breve basata sulle evidenze recuperate."}}}}}
	case "create_xlsx":
		return []ToolCallExample{{Arguments: map[string]any{"filename": "riepilogo-aura.xlsx", "sheets": []any{map[string]any{"name": "Sintesi", "rows": []any{[]any{"Voce", "Valore"}, []any{"Fonti", "3"}}}}}}}
	case "create_pdf":
		return []ToolCallExample{{Arguments: map[string]any{"filename": "riepilogo-aura.pdf", "title": "Riepilogo Aura", "blocks": []any{map[string]any{"kind": "heading", "level": 1, "text": "Riepilogo Aura"}, map[string]any{"kind": "paragraph", "text": "Sintesi breve."}}}}}
	case "execute_code":
		return []ToolCallExample{{Arguments: map[string]any{"code": "from pathlib import Path\nPath('/tmp/aura_out/result.txt').write_text('ok')\nprint('ok')"}}}
	case "list_sources":
		return []ToolCallExample{{Arguments: map[string]any{"limit": 10, "status": "ingested"}}}
	case "read_source":
		return []ToolCallExample{{Arguments: map[string]any{"id": "src_0123456789abcdef", "path": "extract.md", "max_bytes": 4000}}}
	case "store_source":
		return []ToolCallExample{{Arguments: map[string]any{"filename": "nota.txt", "content": "Nota da salvare come fonte.", "mime_type": "text/plain"}}}
	case "ingest_source":
		return []ToolCallExample{{Arguments: map[string]any{"id": "src_0123456789abcdef"}}}
	case "ocr_source":
		return []ToolCallExample{{Arguments: map[string]any{"id": "src_0123456789abcdef"}}}
	case "lint_sources":
		return []ToolCallExample{{Arguments: map[string]any{"limit": 20}}}
	case "web":
		return []ToolCallExample{
			{Arguments: map[string]any{"action": "search", "query": "OpenAI API latest structured outputs", "max_results": 5}},
			{Arguments: map[string]any{"action": "fetch", "url": "https://example.com"}},
		}
	case "task":
		return []ToolCallExample{
			{Arguments: map[string]any{"action": "schedule", "name": "health-check", "kind": "reminder", "payload": "Fai un breve health check di Aura.", "in": "1h"}},
			{Arguments: map[string]any{"action": "list", "status": "active"}},
			{Arguments: map[string]any{"action": "cancel", "name": "health-check"}},
			{Arguments: map[string]any{"action": "run_now", "name": "morning-watch"}},
		}
	case "daily_briefing":
		return []ToolCallExample{{Arguments: map[string]any{"limit": 8}}}
	case "request_dashboard_token":
		return []ToolCallExample{{Arguments: map[string]any{"ttl_minutes": 60}}}
	case "install_skill":
		return []ToolCallExample{{Arguments: map[string]any{"name": "docx"}}}
	case "delete_skill":
		return []ToolCallExample{{Arguments: map[string]any{"name": "docx"}}}
	case "settings_update":
		return []ToolCallExample{{Arguments: map[string]any{"key": "AURA_AGENT_LOOP_MAX_STEPS", "value": "8"}}}
	case "dev_tool":
		return []ToolCallExample{
			{Arguments: map[string]any{"action": "list"}},
			{Arguments: map[string]any{"action": "read", "name": "csv_cleaner"}},
			{Arguments: map[string]any{"action": "save", "name": "csv_cleaner", "description": "Strip blank rows from a CSV", "code": "import pandas as pd\\ndef run(filepath):\\n    pd.read_csv(filepath).dropna(how='all').to_csv(filepath, index=False)"}},
		}
	case "run_aurabot_swarm":
		return []ToolCallExample{{Arguments: map[string]any{"task": "Analizza la memoria di Aura e riassumi i problemi principali.", "mode": "bounded"}}}
	case "read_swarm_result":
		return []ToolCallExample{{Arguments: map[string]any{"run_id": "swarm_123"}}}
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
		return "aura memoria"
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
		return "contenuto"
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
