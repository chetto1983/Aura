package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ToolSearchTool struct {
	registry *Registry
}

func NewToolSearchTool(registry *Registry) *ToolSearchTool {
	if registry == nil {
		return nil
	}
	return &ToolSearchTool{registry: registry}
}

func (t *ToolSearchTool) Name() string { return "tool_search" }

func (t *ToolSearchTool) Description() string {
	return "Find Aura tools by natural-language capability. Use this before asking for any capability that is not currently visible. Returns compact tool docs; it does not execute tools."
}

func (t *ToolSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language description of the capability needed.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of tools to return. Default 5, maximum 5.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *ToolSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := intArg(args, "limit", 5, 1, 5)
	results := t.registry.Search(query, limit, t.Name())
	out := struct {
		Tools []ToolSearchResult `json:"tools"`
	}{
		Tools: results,
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *ToolSearchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
		Examples: []ToolCallExample{
			{
				Description: "Find tools for running shell diagnostics in the Aura container.",
				Arguments:   map[string]any{"query": "run shell command in container", "limit": 3},
			},
			{
				Description: "Find read-only mail tools before planning an inbox workflow.",
				Arguments:   map[string]any{"query": "search and read email messages", "limit": 5},
			},
		},
	}
}
