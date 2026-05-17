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

	"github.com/aura/aura/internal/stringx"
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
		baseURL: stringx.NormalizeBaseURL(baseURL),
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *SearXNGSearchTool) Name() string { return "web_search" }

func (t *SearXNGSearchTool) Description() string {
	return "Search the web for current information. Returns ranked results with titles, URLs, and snippets. " +
		"Optional filters: category (general|news|science), language (e.g. 'it', 'en', 'all'), and " +
		"time_range (day|week|month|year) for recency-bounded queries."
}

func (t *SearXNGSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The web search query. Bang prefixes work: '!wp einstein' for Wikipedia, '!gh repo' for GitHub.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Max results to return. Defaults to 5, capped at 10.",
				"minimum":     1,
				"maximum":     10,
			},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"general", "news", "science"},
				"description": "Optional: route to a SearXNG category. 'news' for current events, 'science' for academic/arxiv, 'general' (default) for everything else.",
			},
			"language": map[string]any{
				"type":        "string",
				"description": "Optional: ISO language code ('it', 'en', 'fr', ...) or 'all'. Defaults to 'all'. Set to a specific code when the query is clearly in one language to improve recall.",
			},
			"time_range": map[string]any{
				"type":        "string",
				"enum":        []string{"day", "week", "month", "year"},
				"description": "Optional: limit to recent results. Use 'day'/'week' when the query implies recency ('latest', 'today', 'recent', 'oggi', 'ultimi'), 'year' for evergreen-but-fresh queries.",
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
	opts := searchOptions{
		MaxResults: intArg(args, "max_results", 5, 1, 10),
		Category:   strings.TrimSpace(stringArg(args, "category")),
		Language:   strings.TrimSpace(stringArg(args, "language")),
		TimeRange:  strings.TrimSpace(stringArg(args, "time_range")),
	}

	out, err := t.search(ctx, query, opts)
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}
	if len(out.Results) > opts.MaxResults {
		out.Results = out.Results[:opts.MaxResults]
	}
	return truncateForToolContext(formatSearchResults(query, out.Results), maxWebToolChars), nil
}

// searchOptions captures the SearXNG-side knobs the LLM can flip.
// Research 2026-05-12: category/language/time_range are the three knobs
// with the highest signal-per-LOC; safesearch stays at 0 (personal-agent
// default — 1+ strips legitimate StackOverflow / medical results).
type searchOptions struct {
	MaxResults int
	Category   string
	Language   string
	TimeRange  string
}

func (t *SearXNGSearchTool) search(ctx context.Context, query string, opts searchOptions) (webSearchResponse, error) {
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
	q.Set("safesearch", "0")
	if opts.Category != "" {
		q.Set("categories", opts.Category)
	}
	if opts.Language != "" {
		q.Set("language", opts.Language)
	}
	if opts.TimeRange != "" {
		q.Set("time_range", opts.TimeRange)
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return webSearchResponse{}, fmt.Errorf("build SearXNG request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	// SearXNG's botdetection limiter regexes Go-http-client / curl /
	// python-requests user-agents into 429. Identify as the Aura bot —
	// trips through cleanly on a self-hosted instance, and the operator
	// gets a meaningful tag in their access log.
	req.Header.Set("User-Agent", "AuraBot/1.0 (+https://github.com/aura/aura)")

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
