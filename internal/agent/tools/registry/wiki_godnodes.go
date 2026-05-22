package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aura/aura/internal/wiki"
)

const (
	godNodesDefaultTopK = 10
	godNodesMinTopK     = 1
	godNodesMaxTopK     = 50
)

// RecallGodNodesTool lists the most-connected wiki pages by degree centrality
// (in+out edges). Backed by the in-memory GraphIndex — no disk scan.
type RecallGodNodesTool struct {
	store *wiki.Store
}

// NewRecallGodNodesTool returns a recall_god_nodes tool. Returns nil when
// store is nil so the registry can skip it gracefully.
func NewRecallGodNodesTool(store *wiki.Store) *RecallGodNodesTool {
	if store == nil {
		return nil
	}
	return &RecallGodNodesTool{store: store}
}

func (t *RecallGodNodesTool) Name() string { return "recall_god_nodes" }

func (t *RecallGodNodesTool) Description() string {
	return "List the most-connected wiki pages by degree centrality. Returns top hub concepts in the knowledge graph - use as project overview when starting a session. Optional: top_k (default 10, max 50)."
}

func (t *RecallGodNodesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"top_k": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Number of top nodes to return (default %d, max %d).", godNodesDefaultTopK, godNodesMaxTopK),
				"minimum":     godNodesMinTopK,
				"maximum":     godNodesMaxTopK,
			},
		},
	}
}

func (t *RecallGodNodesTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: VisibilityActiveTurn,
	}
}

func (t *RecallGodNodesTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("recall_god_nodes: tool unavailable")
	}
	topK := intArg(args, "top_k", godNodesDefaultTopK, godNodesMinTopK, godNodesMaxTopK)
	nodes := t.store.TopNodes(topK)

	type nodeResult struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		Category string `json:"category,omitempty"`
		In       int    `json:"in"`
		Out      int    `json:"out"`
		Total    int    `json:"total"`
	}
	results := make([]nodeResult, len(nodes))
	for i, n := range nodes {
		results[i] = nodeResult{
			Slug:     n.Slug,
			Title:    n.Title,
			Category: n.Category,
			In:       n.InDegree,
			Out:      n.OutDegree,
			Total:    n.TotalDegree,
		}
	}
	if len(results) == 0 {
		return "no wiki pages indexed yet", nil
	}
	data, err := json.Marshal(map[string]any{"nodes": results})
	if err != nil {
		return "", fmt.Errorf("recall_god_nodes: marshal: %w", err)
	}
	return string(data), nil
}

var _ Tool = (*RecallGodNodesTool)(nil)
