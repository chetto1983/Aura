// Package embeddings provides Aura's OpenAI-compatible embedding transport.
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

const (
	// DefaultModel is the model label used by Aura's local embedding sidecar.
	DefaultModel = "aura-local-embedding"
	// DefaultBatchSize bounds one OpenAI-compatible embedding request.
	DefaultBatchSize = 32
	// DefaultTimeout bounds one embedding HTTP request.
	DefaultTimeout   = 60 * time.Second
	maxResponseBytes = 16 << 20
)

// Embedder is the common embedding seam used by memory, documents, and the
// reasoning classifier.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// Client calls an OpenAI-compatible /v1/embeddings endpoint.
type Client struct {
	BaseURL    string
	Model      string
	APIKey     string
	Client     *http.Client
	Dimensions int
	BatchSize  int
	Timeout    time.Duration
}

type embeddingRequest struct {
	Input      []string `json:"input"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     *int      `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed returns one validated vector per input in input order. Empty input does
// not issue an HTTP request.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if c == nil {
		return nil, fmt.Errorf("embeddings: no client configured")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, fmt.Errorf("embeddings: base URL is empty")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	dimensions := c.Dimensions
	if dimensions <= 0 {
		dimensions = config.DefaultEmbedDimensions
	}
	batchSize := c.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := min(start+batchSize, len(texts))
		batch, err := c.embedBatch(ctx, texts[start:end], dimensions)
		if err != nil {
			return nil, fmt.Errorf("embeddings: batch %d: %w", start/batchSize, err)
		}
		out = append(out, batch...)
	}
	return out, nil
}

func (c *Client) embedBatch(ctx context.Context, texts []string, dimensions int) ([][]float64, error) {
	payload := embeddingRequest{Input: texts, Model: modelOrDefault(c.Model)}
	apiKey := strings.TrimSpace(c.APIKey)
	if apiKey != "" {
		// Hosted embedders can MRL-truncate server-side. llama.cpp currently ignores
		// this field, so local responses are narrowed and renormalized below.
		payload.Dimensions = dimensions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint(c.BaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: c.requestTimeout()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("endpoint returned HTTP %d (%s)", resp.StatusCode, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	var decoded embeddingResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("endpoint returned %d embeddings for %d inputs", len(decoded.Data), len(texts))
	}

	out := make([][]float64, len(texts))
	seen := make([]bool, len(texts))
	for _, item := range decoded.Data {
		if item.Index == nil {
			return nil, fmt.Errorf("response index is missing")
		}
		index := *item.Index
		if index < 0 || index >= len(out) {
			return nil, fmt.Errorf("response index %d is out of range", index)
		}
		if seen[index] {
			return nil, fmt.Errorf("response index %d is duplicated", index)
		}
		vector, err := TruncateMRL(item.Embedding, dimensions)
		if err != nil {
			return nil, fmt.Errorf("embedding %d: %w", index, err)
		}
		seen[index] = true
		out[index] = vector
	}
	for index, present := range seen {
		if !present {
			return nil, fmt.Errorf("response index %d is missing", index)
		}
	}
	return out, nil
}

func (c *Client) requestTimeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	if c.Client != nil && c.Client.Timeout > 0 {
		return c.Client.Timeout
	}
	return DefaultTimeout
}

func endpoint(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(base, "/embeddings") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/embeddings"
	}
	return base + "/v1/embeddings"
}

func modelOrDefault(model string) string {
	if model = strings.TrimSpace(model); model != "" {
		return model
	}
	return DefaultModel
}

// TruncateMRL keeps the leading dimensions of a wider Matryoshka vector and
// renormalizes the result. A narrower vector is a model-contract failure.
func TruncateMRL(vector []float64, dimensions int) ([]float64, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("dimension must be positive")
	}
	if len(vector) == dimensions {
		return vector, nil
	}
	if len(vector) < dimensions {
		return nil, fmt.Errorf("has dimension %d, want %d", len(vector), dimensions)
	}
	out := make([]float64, dimensions)
	copy(out, vector[:dimensions])
	var sum float64
	for _, value := range out {
		sum += value * value
	}
	if sum == 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return nil, fmt.Errorf("truncated to %d invalid components", dimensions)
	}
	norm := math.Sqrt(sum)
	for index := range out {
		out[index] /= norm
	}
	return out, nil
}
