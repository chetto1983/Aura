package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aura/aura/internal/wiki"
)

const (
	wikiSurprisesDefaultTopK = 5
	wikiSurprisesMaxTopK     = 20
)

type WikiSurprisesTool struct {
	store *wiki.Store
}

func NewWikiSurprisesTool(store *wiki.Store) *WikiSurprisesTool {
	if store == nil {
		return nil
	}
	return &WikiSurprisesTool{store: store}
}

func (t *WikiSurprisesTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("wiki_surprises: tool unavailable")
	}
	topK := intArg(args, "top_k", wikiSurprisesDefaultTopK, 1, wikiSurprisesMaxTopK)
	items := t.store.SurprisingEdges(topK)
	if items == nil {
		items = []wiki.Surprise{}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("wiki_surprises: marshal: %w", err)
	}
	return string(data), nil
}
