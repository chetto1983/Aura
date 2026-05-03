package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/wiki"
)

// WriteWikiTool writes structured knowledge to the wiki.
type WriteWikiTool struct {
	store  *wiki.Store
	search *search.Engine
}

func NewWriteWikiTool(store *wiki.Store, searchEngine *search.Engine) *WriteWikiTool {
	return &WriteWikiTool{store: store, search: searchEngine}
}

func (t *WriteWikiTool) Name() string { return "write_wiki" }

func (t *WriteWikiTool) Description() string {
	return "Write or update a wiki page for durable memory. Use this only for facts, preferences, decisions, and knowledge worth remembering."
}

func (t *WriteWikiTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short descriptive page title.",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Markdown body. Use [[slug]] links for related wiki pages when helpful.",
			},
			"tags": map[string]any{
				"type":        "array",
				"description": "Optional tags. Use at most 10 short tags.",
				"maxItems":    10,
				"items":       map[string]any{"type": "string"},
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Optional category.",
			},
			"related": map[string]any{
				"type":        "array",
				"description": "Optional related page slugs.",
				"items":       map[string]any{"type": "string"},
			},
			"sources": map[string]any{
				"type":        "array",
				"description": "Optional source URLs or references. Use at most 10.",
				"maxItems":    10,
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"title", "body"},
	}
}

func (t *WriteWikiTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	title, err := requiredString(args, "title")
	if err != nil {
		return "", err
	}
	body, err := requiredString(args, "body")
	if err != nil {
		return "", err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		Tags:          stringSliceArg(args, "tags"),
		Category:      strings.TrimSpace(stringArg(args, "category")),
		Related:       stringSliceArg(args, "related"),
		Sources:       stringSliceArg(args, "sources"),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := t.store.WritePage(ctx, page); err != nil {
		return "", fmt.Errorf("write_wiki: %w", err)
	}

	slug := wiki.Slug(title)
	if t.search != nil {
		if err := t.search.ReindexWikiPage(ctx, slug); err != nil {
			return fmt.Sprintf("Wrote wiki page [[%s]], but re-indexing failed: %v", slug, err), nil
		}
	}

	return fmt.Sprintf("Wrote wiki page [[%s]]: %s", slug, title), nil
}

// ReadWikiTool reads a wiki page by slug.
type ReadWikiTool struct {
	store *wiki.Store
}

func NewReadWikiTool(store *wiki.Store) *ReadWikiTool {
	return &ReadWikiTool{store: store}
}

func (t *ReadWikiTool) Name() string { return "read_wiki" }

func (t *ReadWikiTool) Description() string {
	return "Read a wiki page by slug."
}

func (t *ReadWikiTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{
				"type":        "string",
				"description": "Wiki page slug to read.",
			},
		},
		"required": []string{"slug"},
	}
}

func (t *ReadWikiTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	slug, err := requiredString(args, "slug")
	if err != nil {
		return "", err
	}
	page, err := t.store.ReadPage(slug)
	if err != nil {
		return "", fmt.Errorf("read_wiki: %w", err)
	}
	return formatWikiPage(slug, page), nil
}

// SearchWikiTool searches indexed wiki pages.
type SearchWikiTool struct {
	search *search.Engine
}

func NewSearchWikiTool(searchEngine *search.Engine) *SearchWikiTool {
	return &SearchWikiTool{search: searchEngine}
}

func (t *SearchWikiTool) Name() string { return "search_wiki" }

func (t *SearchWikiTool) Description() string {
	return "Search the wiki for relevant saved knowledge."
}

func (t *SearchWikiTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query for wiki knowledge.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchWikiTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	if !t.search.IsIndexed() {
		return "Wiki search is not indexed yet.", nil
	}
	results, err := t.search.Search(ctx, query, 5)
	if err != nil {
		return "", fmt.Errorf("search_wiki: %w", err)
	}
	if len(results) == 0 {
		return "No wiki results found.", nil
	}
	return search.FormatResults(results), nil
}

func formatWikiPage(slug string, page *wiki.Page) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", page.Title)
	fmt.Fprintf(&sb, "Slug: [[%s]]\n", slug)
	if page.Category != "" {
		fmt.Fprintf(&sb, "Category: %s\n", page.Category)
	}
	if len(page.Tags) > 0 {
		fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(page.Tags, ", "))
	}
	if len(page.Related) > 0 {
		fmt.Fprintf(&sb, "Related: [[%s]]\n", strings.Join(page.Related, "]], [["))
	}
	if len(page.Sources) > 0 {
		fmt.Fprintf(&sb, "Sources: %s\n", strings.Join(page.Sources, ", "))
	}
	sb.WriteString("\n")
	sb.WriteString(page.Body)
	return sb.String()
}

func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return cleanStrings(x)
	case []any:
		values := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return cleanStrings(values)
	default:
		return nil
	}
}

func cleanStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}
