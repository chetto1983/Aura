package tools

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aura/aura/internal/storage/sources/store"
)

// ReadSourceTool reads source metadata or extracted markdown.
type ReadSourceTool struct {
	store source.Repository
}

func NewReadSourceTool(store source.Repository) *ReadSourceTool {
	return &ReadSourceTool{store: store}
}

func (t *ReadSourceTool) Name() string { return "read_source" }

func (t *ReadSourceTool) Description() string {
	return "Read source metadata or extracted markdown by source ID. Modes: metadata, ocr (full ocr.md, capped at 8000 chars), excerpt (first ~4000 chars)."
}

func (t *ReadSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "string",
				"description": "Source ID (e.g. src_<16hex>).",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "metadata, ocr, or excerpt. Defaults to excerpt.",
				"enum":        []string{"metadata", "ocr", "excerpt"},
			},
		},
		"required": []string{"source_id"},
	}
}

func (t *ReadSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("read_source: source store unavailable")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}
	mode := stringArg(args, "mode")
	if mode == "" {
		mode = "excerpt"
	}

	src, err := t.store.Get(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read_source: source %s not found", id)
		}
		return "", fmt.Errorf("read_source: %w", err)
	}

	switch mode {
	case "metadata":
		return formatSourceMetadata(src), nil
	case "ocr":
		return readSourceMarkdown(t.store, src, maxSourceToolChars)
	case "excerpt":
		return readSourceMarkdown(t.store, src, excerptDefaultBytes)
	default:
		return "", fmt.Errorf("read_source: unsupported mode %q", mode)
	}
}
