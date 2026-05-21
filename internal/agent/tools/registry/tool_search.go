package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolSearchTool lets the agent discover tools by natural-language query.
// Backed by the registry's hybrid Search (BM25 lexical + vector cosine via
// embeddinggemma when configured).
//
// Pattern: the system prompt advertises a 1-line manifest of every
// registered tool (name + short description). When the model knows what
// it wants to do but is unsure which tool fits, it calls tool_search to
// retrieve full schemas for the top-K matches. The agent loop then
// permissive-loads those tools into the per-turn pool so the next LLM
// step can invoke them.
//
// Hardware reality (2026-05-12 benchmark): CPU-only mini-LLM rerankers
// (gemma-3-1b, qwen2-0.5b) exceed 1500ms p95 on this box and are not
// viable as the routing backend. embedding cosine via the existing
// aura-llama-embed sidecar serves the same need at ~80ms p95.
type ToolSearchTool struct {
	registry *Registry
}

// NewToolSearchTool returns a tool_search backed by the given registry.
// Returns nil when registry is nil so the caller can skip Register cleanly.
func NewToolSearchTool(registry *Registry) *ToolSearchTool {
	if registry == nil {
		return nil
	}
	return &ToolSearchTool{registry: registry}
}

func (t *ToolSearchTool) Name() string { return "tool_search" }

func (t *ToolSearchTool) Description() string {
	return "Search Aura's tool catalog by natural-language query and return the top matches with full input schemas. Use this when the system-prompt manifest lists a tool you'd like to call but you need the parameter shape, OR when you're unsure which tool fits the user's intent. Returns name, description, and input_schema for up to 10 candidates ranked by relevance. After calling tool_search, you can directly invoke any returned tool — the agent loop loads its schema into your tool pool for the rest of this turn."
}

func (t *ToolSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language description of what the user wants to do or which capability you need. Example: 'genera file Word con sommario', 'cerca email da X', 'database tabelle'. Pass the user's intent verbatim when possible; the retriever is multilingual.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max tools to return. Range 1-10, default 5. Use higher values when the query is ambiguous.",
			},
		},
		"required": []string{"query"},
	}
}

// Definition exposes curated examples and metadata to the prompt assembler.
func (t *ToolSearchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: VisibilityAlwaysOn,
		Examples: []ToolCallExample{
			{
				Description: "User asks for a Word document with a news summary; you need the doc tool schema.",
				Arguments:   map[string]any{"query": "genera file Word con sommario notizie", "limit": 5},
			},
			{
				Description: "User wants to read recent emails; you need the mail-related MCP tools.",
				Arguments:   map[string]any{"query": "leggi mail recenti", "limit": 5},
			},
			{
				Description: "Ambiguous request — return a wider candidate set.",
				Arguments:   map[string]any{"query": "salva questa pagina", "limit": 10},
			},
		},
	}
}

func (t *ToolSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query := stringArg(args, "query")
	if query == "" {
		return "", fmt.Errorf("tool_search: query is required")
	}
	limit := intArg(args, "limit", 5, 1, 10)

	results := t.registry.Search(query, limit)
	if len(results) == 0 {
		return fmt.Sprintf("No tools matched %q. The manifest in the system prompt lists every registered tool — try a more specific query, or call the tool directly by name if you saw it in the manifest.", query), nil
	}

	// Emit as JSON so the agent can parse names + schemas programmatically.
	// Wrapped in a small markdown header for human-readable logs.
	out := struct {
		Query string             `json:"query"`
		Hits  []toolSearchOutHit `json:"hits"`
	}{
		Query: query,
		Hits:  make([]toolSearchOutHit, 0, len(results)),
	}
	for _, r := range results {
		out.Hits = append(out.Hits, toolSearchOutHit{
			Name:        r.Name,
			Description: strings.TrimSpace(r.Description),
			Parameters:  r.InputSchema,
			Score:       r.Score,
		})
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("tool_search: marshal results: %w", err)
	}
	return string(encoded), nil
}

type toolSearchOutHit struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"input_schema,omitempty"`
	Score       int            `json:"score,omitempty"`
}
