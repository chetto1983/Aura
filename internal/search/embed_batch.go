package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type openAICompatBatchEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func NewOpenAICompatBatchEmbeddingFunction(baseURL, apiKey, model string, client *http.Client) BatchEmbeddingFunction {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	if baseURL == "" || apiKey == "" || model == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	embedder := &openAICompatBatchEmbedder{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    client,
	}
	return embedder.Embed
}

func (e *openAICompatBatchEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("batch embedder unavailable")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	body := map[string]any{
		"model": e.model,
		"input": texts,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("batch embeddings request: %w", err)
	}
	defer resp.Body.Close()
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if readErr != nil {
		return nil, fmt.Errorf("batch embeddings read response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("batch embeddings returned %s: %s", resp.Status, strings.TrimSpace(string(respBytes)))
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("batch embeddings decode response: %w", err)
	}
	vectors := make([][]float32, len(texts))
	for fallbackIndex, item := range out.Data {
		index := item.Index
		if index < 0 || index >= len(vectors) {
			index = fallbackIndex
		}
		if index >= 0 && index < len(vectors) {
			vectors[index] = item.Embedding
		}
	}
	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("batch embeddings missing vector at index %d", i)
		}
	}
	return vectors, nil
}
