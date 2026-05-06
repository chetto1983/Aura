package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type searxngResponse struct {
	Results []searxngResult `json:"results"`
}

type searxngResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func main() {
	baseURL := flag.String("base-url", envDefault("SEARXNG_BASE_URL", "http://127.0.0.1:8088"), "SearXNG base URL")
	query := flag.String("q", "Aura second brain agent", "search query")
	maxResults := flag.Int("max-results", 5, "maximum results to print")
	timeout := flag.Duration("timeout", 10*time.Second, "request timeout")
	jsonOut := flag.Bool("json", false, "print raw parsed JSON response")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	rep, elapsed, err := search(ctx, *baseURL, *query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	if *maxResults > 0 && len(rep.Results) > *maxResults {
		rep.Results = rep.Results[:*maxResults]
	}

	if *jsonOut {
		out := struct {
			BaseURL   string          `json:"base_url"`
			Query     string          `json:"query"`
			ElapsedMS int64           `json:"elapsed_ms"`
			Results   []searxngResult `json:"results"`
		}{
			BaseURL:   strings.TrimRight(*baseURL, "/"),
			Query:     *query,
			ElapsedMS: elapsed.Milliseconds(),
			Results:   rep.Results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}

	fmt.Printf("base_url=%s elapsed_ms=%d results=%d\n\n", strings.TrimRight(*baseURL, "/"), elapsed.Milliseconds(), len(rep.Results))
	fmt.Print(formatResults(*query, rep))
}

func search(ctx context.Context, baseURL, query string) (searxngResponse, time.Duration, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return searxngResponse{}, 0, fmt.Errorf("base URL is required")
	}
	if strings.TrimSpace(query) == "" {
		return searxngResponse{}, 0, fmt.Errorf("query is required")
	}

	endpoint, err := url.Parse(baseURL + "/search")
	if err != nil {
		return searxngResponse{}, 0, fmt.Errorf("parse base URL: %w", err)
	}
	q := endpoint.Query()
	q.Set("q", query)
	q.Set("format", "json")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return searxngResponse{}, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return searxngResponse{}, elapsed, fmt.Errorf("request SearXNG: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return searxngResponse{}, elapsed, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return searxngResponse{}, elapsed, fmt.Errorf("SearXNG returned HTTP %d: %s", resp.StatusCode, snippet(body, 300))
	}

	var out searxngResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return searxngResponse{}, elapsed, fmt.Errorf("decode SearXNG JSON: %w", err)
	}
	return out, elapsed, nil
}

func formatResults(query string, rep searxngResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SearXNG results for %q:\n", query)
	if len(rep.Results) == 0 {
		sb.WriteString("No results found.\n")
		return sb.String()
	}
	for i, result := range rep.Results {
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

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func snippet(body []byte, max int) string {
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
