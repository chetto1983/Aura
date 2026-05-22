package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/storage/sources/store"
)

// ListSourcesTool lists sources matching optional kind/status filters.
type ListSourcesTool struct {
	store source.Reader
}

func NewListSourcesTool(store source.Reader) *ListSourcesTool {
	return &ListSourcesTool{store: store}
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
