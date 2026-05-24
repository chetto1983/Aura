package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// ModelMetadata holds context-window metadata returned by the provider's /models endpoint.
type ModelMetadata struct {
	ID                  string
	ContextLength       int
	MaxCompletionTokens int
}

// modelMetadataCache caches fetched metadata keyed by "<baseURL>\x00<modelID>"
// so different base URLs are isolated and each model ID is fetched at most once.
var modelMetadataCache sync.Map

// FetchModelMetadata fetches model metadata from <baseURL>/models and returns
// the entry whose id field matches modelID.
//
// Results are cached in memory: a second call for the same (baseURL, modelID)
// pair returns the cached value without an HTTP round-trip. On a 404 or when
// modelID is not found in the response, an error is returned.
func (c *OpenAIClient) FetchModelMetadata(ctx context.Context, modelID string) (*ModelMetadata, error) {
	cacheKey := c.baseURL + "\x00" + modelID
	if v, ok := modelMetadataCache.Load(cacheKey); ok {
		return v.(*ModelMetadata), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("building /models request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GET /models returned status %d: %s", resp.StatusCode, body)
	}

	var envelope struct {
		Data []struct {
			ID                  string `json:"id"`
			ContextLength       int    `json:"context_length"`
			MaxCompletionTokens int    `json:"max_completion_tokens"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding /models response: %w", err)
	}

	for _, item := range envelope.Data {
		if item.ID == modelID {
			meta := &ModelMetadata{
				ID:                  item.ID,
				ContextLength:       item.ContextLength,
				MaxCompletionTokens: item.MaxCompletionTokens,
			}
			modelMetadataCache.Store(cacheKey, meta)
			return meta, nil
		}
	}
	return nil, fmt.Errorf("model %q not found in /models response (%d entries)", modelID, len(envelope.Data))
}
