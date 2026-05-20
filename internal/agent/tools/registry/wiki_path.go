package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/wiki"
)

const (
	wikiPathDefaultMaxHops = 5
	wikiPathMinMaxHops     = 1
	wikiPathMaxMaxHops     = 20
)

// WikiPathTool finds the shortest undirected path between two wiki slugs using
// BFS over the in-memory GraphIndex.
type WikiPathTool struct {
	store *wiki.Store
}

// NewWikiPathTool returns a wiki_path tool. Returns nil when store is nil.
func NewWikiPathTool(store *wiki.Store) *WikiPathTool {
	if store == nil {
		return nil
	}
	return &WikiPathTool{store: store}
}

func (t *WikiPathTool) Name() string { return "wiki_path" }

func (t *WikiPathTool) Description() string {
	return "Find the shortest path between two wiki pages via their [[wiki-link]] / related: edges. Returns the slug chain connecting them (e.g. 'X -> Y -> Z (2 hops)') or reports that no path exists within the hop limit. Useful for answering 'how is concept X connected to concept Y?'."
}

func (t *WikiPathTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from_slug": map[string]any{
				"type":        "string",
				"description": "Slug of the starting wiki page.",
			},
			"to_slug": map[string]any{
				"type":        "string",
				"description": "Slug of the destination wiki page.",
			},
			"max_hops": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum hops to traverse (default %d, max %d).", wikiPathDefaultMaxHops, wikiPathMaxMaxHops),
				"minimum":     wikiPathMinMaxHops,
				"maximum":     wikiPathMaxMaxHops,
			},
		},
		"required": []string{"from_slug", "to_slug"},
	}
}

func (t *WikiPathTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: VisibilityActiveTurn,
	}
}

func (t *WikiPathTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("wiki_path: tool unavailable")
	}
	from := stringArg(args, "from_slug")
	to := stringArg(args, "to_slug")
	if from == "" || to == "" {
		return "", fmt.Errorf("wiki_path: from_slug and to_slug are required")
	}
	maxHops := intArg(args, "max_hops", wikiPathDefaultMaxHops, wikiPathMinMaxHops, wikiPathMaxMaxHops)

	path := t.store.WikiPath(from, to, maxHops)

	type result struct {
		Path      []string `json:"path"`
		Hops      int      `json:"hops,omitempty"`
		Narration string   `json:"narration"`
	}

	if len(path) == 0 {
		data, err := json.Marshal(result{
			Narration: fmt.Sprintf("no path within %d hops", maxHops),
		})
		if err != nil {
			return "", fmt.Errorf("wiki_path: marshal: %w", err)
		}
		return string(data), nil
	}

	hops := len(path) - 1
	narration := fmt.Sprintf("%s (%d hop", strings.Join(path, " -> "), hops)
	if hops != 1 {
		narration += "s"
	}
	narration += ")"

	data, err := json.Marshal(result{
		Path:      path,
		Hops:      hops,
		Narration: narration,
	})
	if err != nil {
		return "", fmt.Errorf("wiki_path: marshal: %w", err)
	}
	return string(data), nil
}

var _ Tool = (*WikiPathTool)(nil)
