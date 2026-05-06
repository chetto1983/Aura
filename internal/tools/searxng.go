package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxSearXNGResponseBytes = 2 << 20

// SearXNGSearchTool calls a SearXNG JSON API endpoint while preserving the
// stable LLM-facing web_search tool name.
type SearXNGSearchTool struct {
	baseURL string
	client  *http.Client
}

// NewSearXNGSearchTool creates a SearXNG-backed web_search tool.
func NewSearXNGSearchTool(baseURL string) *SearXNGSearchTool {
	return &SearXNGSearchTool{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *SearXNGSearchTool) Name() string { return "web_search" }

func (t *SearXNGSearchTool) Description() string {
	return "Search the web for current information and return relevant results with titles, URLs, and snippets."
}

func (t *SearXNGSearchTool) Parameters() map[string]any {
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

func (t *SearXNGSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	maxResults := intArg(args, "max_results", 5, 1, 10)

	out, err := t.search(ctx, query)
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}
	if len(out.Results) > maxResults {
		out.Results = out.Results[:maxResults]
	}
	return truncateForToolContext(formatSearchResults(query, out.Results), maxWebToolChars), nil
}

func (t *SearXNGSearchTool) search(ctx context.Context, query string) (webSearchResponse, error) {
	if t.baseURL == "" {
		return webSearchResponse{}, fmt.Errorf("SearXNG base URL is required")
	}
	endpoint, err := url.Parse(t.baseURL + "/search")
	if err != nil {
		return webSearchResponse{}, fmt.Errorf("parse SearXNG base URL: %w", err)
	}
	q := endpoint.Query()
	q.Set("q", query)
	q.Set("format", "json")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return webSearchResponse{}, fmt.Errorf("build SearXNG request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return webSearchResponse{}, fmt.Errorf("request SearXNG: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSearXNGResponseBytes))
	if err != nil {
		return webSearchResponse{}, fmt.Errorf("read SearXNG response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(detail), "json") {
			detail += " (SearXNG JSON API may be disabled; enable formats: [json] in settings.yml)"
		}
		return webSearchResponse{}, fmt.Errorf("SearXNG returned HTTP %d: %s", resp.StatusCode, detail)
	}

	var out webSearchResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return webSearchResponse{}, fmt.Errorf("decode SearXNG JSON: %w", err)
	}
	return out, nil
}
