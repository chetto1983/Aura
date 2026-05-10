package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxWebToolChars = 8000

type webSearchResponse struct {
	Results []webSearchResult `json:"results"`
}

type webFetchResponse struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Links   []string `json:"links"`
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
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

func truncateForToolContext(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	return strings.TrimSpace(s[:maxChars]) + "\n\n[truncated]"
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

func boolArg(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes":
			return true
		default:
			return false
		}
	default:
		return false
	}
}
