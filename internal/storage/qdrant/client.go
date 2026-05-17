package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aura/aura/internal/httputil"
	"github.com/aura/aura/internal/stringx"
)

const defaultQdrantBatchSize = 64

// Client is the shared Qdrant REST client interface.
type Client interface {
	Health(ctx context.Context) error
	Search(ctx context.Context, collection string, vector []float32, topK int, withPayload bool) ([]ScoredPoint, error)
	Upsert(ctx context.Context, collection string, points []Point) error
	Delete(ctx context.Context, collection string, ids []string) error
	CreateCollection(ctx context.Context, collection string, vectorSize int) error
	DeleteCollection(ctx context.Context, collection string) error
	CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error)
}

// httpClient is the concrete HTTP-based Qdrant client.
type httpClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates a new Qdrant Client from the given Config.
// Returns an error if cfg.BaseURL is empty after trimming.
func NewClient(cfg Config) (Client, error) {
	base := stringx.NormalizeBaseURL(cfg.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("qdrant: BaseURL is required")
	}
	return &httpClient{
		baseURL: base,
		apiKey:  cfg.APIKey,
		http:    httputil.NewHTTPClient(cfg.Timeout, 30*time.Second),
	}, nil
}

// String returns the baseURL for use in diagnostic messages.
func (c *httpClient) String() string {
	return c.baseURL
}

// Health checks Qdrant's /readyz endpoint and returns nil on 2xx, error otherwise.
func (c *httpClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant health check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("qdrant health check returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Search queries a Qdrant collection with the given vector.
func (c *httpClient) Search(ctx context.Context, collection string, vector []float32, topK int, withPayload bool) ([]ScoredPoint, error) {
	body := map[string]any{
		"query":        vector,
		"limit":        topK,
		"with_payload": withPayload,
	}
	var out struct {
		Status string `json:"status"`
		Result struct {
			Points []ScoredPoint `json:"points"`
		} `json:"result"`
	}
	endpoint := c.baseURL + "/collections/" + url.PathEscape(collection) + "/points/query"
	data, err := c.doRequest(ctx, http.MethodPost, endpoint, body, http.StatusOK)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("qdrant search decode: %w", err)
		}
	}
	if out.Status != "" && out.Status != "ok" {
		return nil, fmt.Errorf("qdrant search returned status %q", out.Status)
	}
	return out.Result.Points, nil
}

// Upsert inserts or updates points in the named collection in batches of 64.
func (c *httpClient) Upsert(ctx context.Context, collection string, points []Point) error {
	collPath := c.baseURL + "/collections/" + url.PathEscape(collection)
	for start := 0; start < len(points); start += defaultQdrantBatchSize {
		end := start + defaultQdrantBatchSize
		if end > len(points) {
			end = len(points)
		}
		body := map[string]any{"points": points[start:end]}
		if _, err := c.doRequest(ctx, http.MethodPut, collPath+"/points?wait=true", body, http.StatusOK); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes points by ID from the named collection in batches of 64.
// Returns nil on 404 (point already absent is treated as success).
func (c *httpClient) Delete(ctx context.Context, collection string, ids []string) error {
	collPath := c.baseURL + "/collections/" + url.PathEscape(collection)
	for start := 0; start < len(ids); start += defaultQdrantBatchSize {
		end := start + defaultQdrantBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		body := map[string]any{"points": ids[start:end]}
		_, err := c.doRequest(ctx, http.MethodPost, collPath+"/points/delete?wait=true", body, http.StatusOK, http.StatusNotFound)
		if err != nil {
			// Treat 404 responses as success (point already gone).
			if isNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// CreateCollection creates a new collection with the given vector size.
// Returns nil if the collection already exists (HTTP 409 Conflict).
func (c *httpClient) CreateCollection(ctx context.Context, collection string, vectorSize int) error {
	body := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	endpoint := c.baseURL + "/collections/" + url.PathEscape(collection)
	_, err := c.doRequest(ctx, http.MethodPut, endpoint, body, http.StatusOK, http.StatusConflict)
	return err
}

// DeleteCollection removes a collection. Returns nil on 404 (already absent).
func (c *httpClient) DeleteCollection(ctx context.Context, collection string) error {
	endpoint := c.baseURL + "/collections/" + url.PathEscape(collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("delete qdrant collection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("delete qdrant collection returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// CollectionInfo returns metadata about a collection.
// On 404, returns zero CollectionInfo (not an error) -- handles the first-startup warm-cache case.
func (c *httpClient) CollectionInfo(ctx context.Context, collection string) (CollectionInfo, error) {
	endpoint := c.baseURL + "/collections/" + url.PathEscape(collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CollectionInfo{}, err
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Collection does not exist yet — not an error (Pitfall 3: warm-cache first-startup).
		return CollectionInfo{}, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if readErr != nil {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info read: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out struct {
		Result CollectionInfo `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return CollectionInfo{}, fmt.Errorf("qdrant collection info decode: %w", err)
	}
	return out.Result, nil
}

// WaitForReady blocks until the Qdrant instance is healthy or timeout expires.
// Uses exponential backoff starting at 500ms, capped at 10s.
// Returns an error with the endpoint URL and total elapsed time on timeout.
func WaitForReady(ctx context.Context, client Client, timeout time.Duration) error {
	start := time.Now()
	deadline := start.Add(timeout)
	backoff := 500 * time.Millisecond
	maxBackoff := 10 * time.Second
	if c, ok := client.(*httpClient); ok && c.http != nil {
		// Use config MaxRetryDelay if available -- not exposed on the interface,
		// so we use the constant default.
		_ = c
	}
	for time.Now().Before(deadline) {
		if err := client.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff = minDuration(backoff*2, maxBackoff)
		}
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	endpoint := ""
	if c, ok := client.(*httpClient); ok {
		endpoint = c.baseURL
	}
	return fmt.Errorf("qdrant not ready after %v at %s (elapsed: %v)", timeout, endpoint, elapsed)
}

// authorize sets the api-key header if the API key is configured.
func (c *httpClient) authorize(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}
}

// doRequest is a reusable HTTP call helper that marshals the body to JSON,
// executes the request, reads the response (limited to 4MB), and validates the
// status code against the accepted list. Returns the raw response body bytes on
// success, or an error describing the status and body on failure.
func (c *httpClient) doRequest(ctx context.Context, method, endpoint string, body any, accepted ...int) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorize(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if readErr != nil {
		return nil, fmt.Errorf("%s %s read response: %w", method, endpoint, readErr)
	}
	for _, code := range accepted {
		if resp.StatusCode == code {
			return respBytes, nil
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBytes, nil
	}
	return nil, fmt.Errorf("%s %s returned %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(respBytes)))
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
