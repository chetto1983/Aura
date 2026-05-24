package tools

import (
	"context"
	"strings"
)

// WebTool consolidates the web verbs (web_search, web_fetch) into a single
// action-enum surface. Picobot pattern: one tool, one action
// enum acting as the verb, so the LLM never has to pick between two
// near-identical web entry points.
//
//	action=search — SearXNG-backed web search, returns ranked results.
//	action=fetch  — fetch a single URL, returns title + main text + links.
//
// The actual transport logic stays in searxng.go and direct_fetch.go;
// WebTool just dispatches. SearXNG base URL is required for search;
// fetch needs no extra config.
type WebTool struct {
	searcher *SearXNGSearchTool
	fetcher  *DirectWebFetchTool
}

// NewWebTool builds the unified web tool. searxBaseURL is required for
// the search action; when empty, action=search returns an error but
// action=fetch still works.
func NewWebTool(searxBaseURL string) *WebTool {
	return &WebTool{
		searcher: NewSearXNGSearchTool(searxBaseURL),
		fetcher:  NewDirectWebFetchTool(),
	}
}

func (t *WebTool) Name() string { return "web" }

func (t *WebTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		OpenWorldHint:  true,
		VisibilityTier: VisibilityActiveTurn,
	}
}

func (t *WebTool) Description() string {
	return `Search web or fetch one URL. Returns top-5 results by default (max 10). Search needs action="search"+query; fetch needs action="fetch"+url and returns ~12 KiB text+links, SSRF-gated.`
}

func (t *WebTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"search", "fetch"},
				"description": `REQUIRED. "search" needs a query, "fetch" needs a url.`,
			},
			"query": map[string]any{
				"type":        "string",
				"description": `Required when action="search". The web search query string.`,
			},
			"max_results": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     10,
				"description": `Optional, action="search" only. Max results to return (default 5).`,
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"general", "news", "science"},
				"description": `Optional, action="search" only. 'news' for current events, 'science' for academic/arxiv, 'general' (default) for everything else.`,
			},
			"language": map[string]any{
				"type":        "string",
				"description": `Optional, action="search" only. ISO language code ('it', 'en', 'fr', 'all').`,
			},
			"time_range": map[string]any{
				"type":        "string",
				"enum":        []string{"day", "week", "month", "year"},
				"description": `Optional, action="search" only. Recency filter - use 'day'/'week' for 'recent'/'latest'/'oggi'.`,
			},
			"url": map[string]any{
				"type":        "string",
				"description": `Required when action="fetch". The URL to fetch (http or https only).`,
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "search", RequiredKeys: []string{"query"}},
			{Name: "fetch", RequiredKeys: []string{"url"}},
		}),
		// JSON Schema "examples" - concrete shapes models read before
		// the description, fixing the schema-mismatch retry pattern.
		"examples": []any{
			map[string]any{"action": "search", "query": "Caraglio cronaca recente"},
			map[string]any{"action": "search", "query": "latest CRISPR research", "category": "science", "time_range": "month"},
			map[string]any{"action": "fetch", "url": "https://example.com/article"},
		},
	}
}

func (t *WebTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	// Auto-recover when the model used an action name as a param key,
	// e.g. web({"search":"X"}) -> web({"action":"search","query":"X"}).
	// Observed live 2026-05-24 turn #158; saves a full retry round.
	if rewritten, ok := RewriteVerbKeyAsAction(args, webValidActions, webActionHints); ok {
		args = rewritten
	}
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "search":
		return t.searcher.Execute(ctx, args)
	case "fetch":
		return t.fetcher.Execute(ctx, args)
	case "":
		return "", ActionRequiredError("web", webValidActions, args, webActionHints, "search")
	default:
		return "", UnknownActionError("web", action, webValidActions, args)
	}
}

var (
	webValidActions = []string{"search", "fetch"}
	webActionHints  = []ActionHint{
		{Name: "fetch", RequiredKeys: []string{"url"}},
		{Name: "search", RequiredKeys: []string{"query"}},
	}
)
