package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WebSearchTool calls Ollama's web_search API.
type WebSearchTool struct {
	client ollamaWebClient
}

// NewWebSearchTool creates an Ollama-backed web_search tool.
func NewWebSearchTool(apiKey, baseURL string) *WebSearchTool {
	return &WebSearchTool{client: newOllamaWebClient(apiKey, baseURL, 20*time.Second)}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for current information and return relevant results with titles, URLs, and snippets."
}

func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The web search query.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Defaults to 5 and is capped at 10.",
				"minimum":     1,
				"maximum":     10,
			},
		},
		"required": []string{"query"},
	}
}

type webSearchResponse struct {
	Results []webSearchResult `json:"results"`
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	maxResults := intArg(args, "max_results", 5, 1, 10)

	payload := map[string]any{
		"query":       query,
		"max_results": maxResults,
	}

	var out webSearchResponse
	if err := t.client.post(ctx, "/web_search", payload, &out); err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}

	return truncateForToolContext(formatSearchResults(query, out.Results), maxWebToolChars), nil
}

func formatSearchResults(query string, results []webSearchResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Web search results for %q:\n", query)
	if len(results) == 0 {
		sb.WriteString("No results found.")
		return sb.String()
	}
	for i, result := range results {
		fmt.Fprintf(&sb, "\n%d. %s\n", i+1, strings.TrimSpace(result.Title))
		if result.URL != "" {
			fmt.Fprintf(&sb, "URL: %s\n", result.URL)
		}
		if result.Content != "" {
			fmt.Fprintf(&sb, "%s\n", strings.TrimSpace(result.Content))
		}
	}
	return sb.String()
}

func requiredString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return strings.TrimSpace(s), nil
}

func intArg(args map[string]any, key string, fallback, min, max int) int {
	v, ok := args[key]
	if !ok {
		return fallback
	}

	var n int
	switch x := v.(type) {
	case int:
		n = x
	case int64:
		n = int(x)
	case float64:
		n = int(x)
	case json.Number:
		parsed, err := x.Int64()
		if err != nil {
			return fallback
		}
		n = int(parsed)
	default:
		return fallback
	}

	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
