package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeResult is what the wizard's "Test connection" button returns. The
// front-end renders ok=false with the error inline so the user can fix
// the typo without a refresh.
type ProbeResult struct {
	OK     bool     `json:"ok"`
	Error  string   `json:"error,omitempty"`
	Models []string `json:"models,omitempty"`
}

// probeProvider hits an OpenAI-compatible /models endpoint to validate
// (base_url, api_key) before we save them. Times out at 6s — providers
// that take longer than that aren't usable for chat anyway. Returns the
// model list so the wizard can offer it as a dropdown.
func probeProvider(ctx context.Context, baseURL, apiKey, probePath string) ProbeResult {
	if strings.TrimSpace(baseURL) == "" {
		return ProbeResult{Error: "base URL is required"}
	}
	url := strings.TrimRight(baseURL, "/")
	if probePath == "" {
		probePath = "/models"
	}
	if !strings.HasPrefix(probePath, "/") {
		probePath = "/" + probePath
	}
	url += probePath

	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ProbeResult{Error: fmt.Sprintf("malformed URL: %v", err)}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ProbeResult{Error: "timed out connecting (>6s) — check the URL and your network"}
		}
		return ProbeResult{Error: fmt.Sprintf("connect failed: %v", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ProbeResult{Error: "authentication failed — check your API key"}
	}
	if resp.StatusCode == http.StatusNotFound {
		return ProbeResult{Error: "endpoint not found — check the base URL"}
	}
	if resp.StatusCode >= 400 {
		return ProbeResult{Error: fmt.Sprintf("provider returned %d: %s", resp.StatusCode, snippet(body))}
	}

	models, parseErr := parseModelList(body)
	if parseErr != nil {
		// Connect succeeded; we just couldn't parse the list. Don't fail
		// the wizard for that — the user might be using a provider with
		// a non-standard /models response shape.
		return ProbeResult{OK: true}
	}
	return ProbeResult{OK: true, Models: models}
}

// modelListResponse covers the OpenAI /v1/models shape ({"data": [{"id": "..."}]}).
// Anthropic returns {"data": [{"id": ..., "display_name": ...}]} which fits.
// Ollama's /v1/models also returns this shape (since v0.1.34).
type modelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func parseModelList(body []byte) ([]string, error) {
	var r modelListResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(r.Data))
	for _, m := range r.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no models")
	}
	return out, nil
}

// detectOllama hits the local Ollama HTTP API and reports whether it's
// running. Used for auto-suggesting the Ollama preset on first run.
func detectOllama(ctx context.Context, host string) bool {
	if host == "" {
		host = "http://localhost:11434"
	}
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(host, "/")+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func snippet(body []byte) string {
	const max = 200
	if len(body) > max {
		return string(body[:max]) + "..."
	}
	return string(body)
}
