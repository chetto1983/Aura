package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/source"
)

// ListSourcesTool lists sources matching optional kind/status filters.
type ListSourcesTool struct {
	store source.Reader
}

func NewListSourcesTool(store source.Reader) *ListSourcesTool {
	return &ListSourcesTool{store: store}
}

func (t *ListSourcesTool) Name() string { return "list_sources" }

func (t *ListSourcesTool) Description() string {
	return "List stored sources, newest first. Optional filters: kind (pdf/text/url/xlsx/docx/pdf_generated/sandbox_artifact), status (stored/ocr_complete/ingested/failed)."
}

func (t *ListSourcesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"description": "Filter by kind.",
				"enum":        []string{"pdf", "text", "url", "xlsx", "docx", "pdf_generated", "sandbox_artifact"},
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Filter by status.",
				"enum":        []string{"stored", "ocr_complete", "ingested", "failed"},
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum sources to return (default 20, max 100).",
				"minimum":     1,
				"maximum":     100,
			},
		},
	}
}

func (t *ListSourcesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("list_sources: source store unavailable")
	}
	filter := source.ListFilter{
		Kind:   source.Kind(stringArg(args, "kind")),
		Status: source.Status(stringArg(args, "status")),
	}
	limit := intArg(args, "limit", 20, 1, 100)

	rows, err := t.store.List(filter)
	if err != nil {
		return "", fmt.Errorf("list_sources: %w", err)
	}
	truncated := false
	if len(rows) > limit {
		rows = rows[:limit]
		truncated = true
	}
	return formatSourceList(rows, filter, truncated), nil
}

// LintSourcesTool reports sources that need attention.
type LintSourcesTool struct {
	store source.Reader
}

func NewLintSourcesTool(store source.Reader) *LintSourcesTool {
	return &LintSourcesTool{store: store}
}

func (t *LintSourcesTool) Name() string { return "lint_sources" }

func (t *LintSourcesTool) Description() string {
	return "Report sources needing attention: stored but not OCRed, OCRed but not ingested, and failed sources."
}

func (t *LintSourcesTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *LintSourcesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.store == nil {
		return "", errors.New("lint_sources: source store unavailable")
	}
	rows, err := t.store.List(source.ListFilter{})
	if err != nil {
		return "", fmt.Errorf("lint_sources: %w", err)
	}
	return formatSourceLint(rows), nil
}
