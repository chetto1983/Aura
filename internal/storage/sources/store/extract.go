package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

const (
	ExtractMarkdownFile = "extract.md"
	ExtractJSONFile     = "extract.json"
)

type ExtractInput struct {
	Source *Source
	Bytes  []byte
}

type ExtractResult struct {
	Markdown string
	Metadata ExtractionMeta
}

type Extractor interface {
	Extract(ctx context.Context, in ExtractInput) (ExtractResult, error)
}

func WriteExtractionFiles(store interface{ Path(id, name string) string }, src *Source, res ExtractResult) error {
	if src == nil {
		return fmt.Errorf("source: nil source")
	}
	mdPath := store.Path(src.ID, ExtractMarkdownFile)
	if mdPath == "" {
		return fmt.Errorf("source: invalid extract markdown path for %s", src.ID)
	}
	if err := os.WriteFile(mdPath, []byte(res.Markdown), 0o644); err != nil {
		return fmt.Errorf("source: write extract.md: %w", err)
	}
	b, err := json.MarshalIndent(res.Metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("source: marshal extract metadata: %w", err)
	}
	jsonPath := store.Path(src.ID, ExtractJSONFile)
	if jsonPath == "" {
		return fmt.Errorf("source: invalid extract json path for %s", src.ID)
	}
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		return fmt.Errorf("source: write extract.json: %w", err)
	}
	return nil
}
