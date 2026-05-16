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
	return `Search the web or fetch a single URL.

Actions (pick one via the "action" field):

  • search — query a SearXNG-backed web index. Returns ranked results
    with title, URL, and snippet. Required: query. Optional: max_results
    (default 5, capped at 10). Use this when the user asks an open-ended
    question that benefits from current information.

  • fetch — download one URL and extract the main text and links.
    Required: url. The fetch is bounded (~2 MiB body, ~12 KiB extracted
    text, 30s timeout) and SSRF-gated: loopback / private / link-local /
    cloud-metadata IPs are refused. Use this when you have a specific
    page in mind — typically as a follow-up to a search result.`
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
				"description": "Which operation: search (web index) or fetch (one URL).",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "search only: the web search query.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     10,
				"description": "search only, optional: max results to return (default 5).",
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"general", "news", "science"},
				"description": "search only, optional: 'news' for current events, 'science' for academic/arxiv, 'general' (default) for everything else.",
			},
			"language": map[string]any{
				"type":        "string",
				"description": "search only, optional: ISO language code ('it', 'en', 'fr', 'all'). Pass when the query is clearly in one language.",
			},
			"time_range": map[string]any{
				"type":        "string",
				"enum":        []string{"day", "week", "month", "year"},
				"description": "search only, optional: recency filter. Use 'day'/'week' for queries implying 'recent'/'latest'/'oggi'.",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "fetch only: the URL to fetch. http and https only.",
			},
		},
	}
}

func (t *WebTool) Execute(ctx context.Context, args map[string]any) (string, error) {
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
