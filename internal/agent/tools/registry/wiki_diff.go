package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aura/aura/internal/wiki"
)

type WikiDiffTool struct {
	store *wiki.Store
}

func NewWikiDiffTool(store *wiki.Store) *WikiDiffTool {
	if store == nil {
		return nil
	}
	return &WikiDiffTool{store: store}
}

func (t *WikiDiffTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("wiki_diff: tool unavailable")
	}
	diff, resolved, err := wiki.DiffStoreFromGitRef(t.store, stringArg(args, "since"))
	if err != nil {
		return "", err
	}
	out := struct {
		Since        string           `json:"since"`
		NewNodes     []string         `json:"new_nodes"`
		RemovedNodes []string         `json:"removed_nodes"`
		NewEdges     []wiki.GraphEdge `json:"new_edges"`
		RemovedEdges []wiki.GraphEdge `json:"removed_edges"`
		Summary      string           `json:"summary"`
	}{
		Since:        resolved,
		NewNodes:     diff.NewNodes,
		RemovedNodes: diff.RemovedNodes,
		NewEdges:     diff.NewEdges,
		RemovedEdges: diff.RemovedEdges,
		Summary:      diff.Summary,
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("wiki_diff: marshal: %w", err)
	}
	return string(data), nil
}
