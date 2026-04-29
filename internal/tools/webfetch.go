package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// WebFetchTool calls Ollama's web_fetch API.
type WebFetchTool struct {
	client ollamaWebClient
}

// NewWebFetchTool creates an Ollama-backed web_fetch tool.
func NewWebFetchTool(apiKey, baseURL string) *WebFetchTool {
	return &WebFetchTool{client: newOllamaWebClient(apiKey, baseURL, 30*time.Second)}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }

func (t *WebFetchTool) Description() string {
	return "Fetch a web page by URL and return its title, main content, and discovered links."
}

func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch.",
			},
		},
		"required": []string{"url"},
	}
}

type webFetchResponse struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Links   []string `json:"links"`
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	targetURL, err := requiredString(args, "url")
	if err != nil {
		return "", err
	}

	var out webFetchResponse
	if err := t.client.post(ctx, "/web_fetch", map[string]any{"url": targetURL}, &out); err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}

	return truncateForToolContext(formatFetchResult(targetURL, out), maxWebToolChars), nil
}

func formatFetchResult(targetURL string, result webFetchResponse) string {
	var sb strings.Builder
	if result.Title != "" {
		fmt.Fprintf(&sb, "# %s\n\n", strings.TrimSpace(result.Title))
	} else {
		fmt.Fprintf(&sb, "# %s\n\n", targetURL)
	}
	if result.Content != "" {
		sb.WriteString(strings.TrimSpace(result.Content))
		sb.WriteString("\n")
	}
	if len(result.Links) > 0 {
		sb.WriteString("\nLinks:\n")
		limit := len(result.Links)
		if limit > 20 {
			limit = 20
		}
		for _, link := range result.Links[:limit] {
			fmt.Fprintf(&sb, "- %s\n", link)
		}
	}
	return sb.String()
}
