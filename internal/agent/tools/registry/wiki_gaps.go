package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aura/aura/internal/wiki"
)

const (
	wikiGapsDefaultTopK = 20
	wikiGapsMaxTopK     = 50
)

type WikiGapsTool struct {
	store *wiki.Store
}

func NewWikiGapsTool(store *wiki.Store) *WikiGapsTool {
	if store == nil {
		return nil
	}
	return &WikiGapsTool{store: store}
}

func (t *WikiGapsTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("wiki_gaps: tool unavailable")
	}
	topK := intArg(args, "top_k", wikiGapsDefaultTopK, 1, wikiGapsMaxTopK)
	gaps := t.store.KnowledgeGaps()
	total := len(gaps)
	slugs := make([]string, 0, min(topK, total))
	for i, gap := range gaps {
		if i >= topK {
			break
		}
		slugs = append(slugs, gap.Slug)
	}
	out := struct {
		Count int      `json:"count"`
		Slugs []string `json:"slugs"`
		Hint  string   `json:"hint"`
	}{
		Count: total,
		Slugs: slugs,
		Hint:  wikiGapsHint(total, len(slugs)),
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("wiki_gaps: marshal: %w", err)
	}
	return string(data), nil
}

func wikiGapsHint(total, shown int) string {
	if total == 0 {
		return "no isolated or under-linked wiki pages found"
	}
	if shown < total {
		return "possible orphan pages with <=1 graph connection; showing the first results"
	}
	return "possible orphan pages with <=1 graph connection"
}
