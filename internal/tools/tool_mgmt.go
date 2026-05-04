package tools

import (
	"context"
	"fmt"
	"strings"
)

// ListToolsTool lets the LLM discover available tools in the registry.
type ListToolsTool struct {
	store ToolStore
}

// NewListToolsTool creates a list_tools tool. Returns nil if store is nil.
func NewListToolsTool(store ToolStore) *ListToolsTool {
	if store == nil {
		return nil
	}
	return &ListToolsTool{store: store}
}

func (t *ListToolsTool) Name() string { return "list_tools" }

func (t *ListToolsTool) Description() string {
	return "List all Python tools in the persistent tool registry. " +
		"Use this before writing new code — a tool may already exist for the task."
}

func (t *ListToolsTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *ListToolsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	tools, err := t.store.ListTools()
	if err != nil {
		return "", fmt.Errorf("list tools: %w", err)
	}
	if len(tools) == 0 {
		return "No tools registered yet.", nil
	}
	var b strings.Builder
	for _, tool := range tools {
		b.WriteString(fmt.Sprintf("- **%s**: %s (params: %s)\n", tool.Name, tool.Description, tool.Params))
	}
	return b.String(), nil
}

// ReadToolTool lets the LLM read a tool's source code.
type ReadToolTool struct {
	store ToolStore
}

// NewReadToolTool creates a read_tool tool. Returns nil if store is nil.
func NewReadToolTool(store ToolStore) *ReadToolTool {
	if store == nil {
		return nil
	}
	return &ReadToolTool{store: store}
}

func (t *ReadToolTool) Name() string { return "read_tool" }

func (t *ReadToolTool) Description() string {
	return "Read the source code of a registered Python tool. " +
		"Use this to understand what an existing tool does before using or modifying it."
}

func (t *ReadToolTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the tool to read",
			},
		},
		"required": []string{"name"},
	}
}

func (t *ReadToolTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return "", fmt.Errorf("tool name is required")
	}
	code, err := t.store.GetToolCode(name)
	if err != nil {
		return "", fmt.Errorf("read tool %s: %w", name, err)
	}
	return code, nil
}

// SaveToolTool lets the LLM persist useful scripts to the tool registry.
type SaveToolTool struct {
	store ToolStore
}

// NewSaveToolTool creates a save_tool tool. Returns nil if store is nil.
func NewSaveToolTool(store ToolStore) *SaveToolTool {
	if store == nil {
		return nil
	}
	return &SaveToolTool{store: store}
}

func (t *SaveToolTool) Name() string { return "save_tool" }

func (t *SaveToolTool) Description() string {
	return "Save a Python script as a permanent tool in the registry. " +
		"Use this after successfully executing code that solves a reusable problem. " +
		"The tool becomes discoverable by list_tools and can be read with read_tool."
}

func (t *SaveToolTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Unique name for the tool (lowercase_underscores)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "One-line description of what the tool does",
			},
			"params": map[string]any{
				"type":        "string",
				"description": "Comma-separated parameter names and types, e.g. 'filepath (str), col1 (str)'",
			},
			"code": map[string]any{
				"type":        "string",
				"description": "Python source code for the tool",
			},
			"usage": map[string]any{
				"type":        "string",
				"description": "When to use this tool, e.g. 'user uploaded a CSV and wants statistics'",
			},
		},
		"required": []string{"name", "description", "code"},
	}
}

func (t *SaveToolTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	params, _ := args["params"].(string)
	code, _ := args["code"].(string)
	usage, _ := args["usage"].(string)

	if err := t.store.SaveTool(ctx, name, description, params, code, usage); err != nil {
		return "", fmt.Errorf("save tool: %w", err)
	}
	return fmt.Sprintf("Tool '%s' saved to registry.", name), nil
}
