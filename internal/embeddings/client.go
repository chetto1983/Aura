// Package embeddings provides Aura's OpenAI-compatible embedding transport.
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
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

// ErrNoCredential is a hosted route asked to embed while its credential is empty. It is the
// route's failure, never the text's, and nothing is sent.
var ErrNoCredential = errors.New("embeddings: the hosted route has no credential")

// StatusError is an answer outside 2xx from the embedding endpoint.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("endpoint returned HTTP %d (%s)", e.Code, e.Status)
}

// RejectsInput reports whether err is the endpoint refusing the input itself: 400, 413 or
// 422. Only those say something about a text; 401, 403 and 429 are about the route or the
// account, and a 5xx is about the server.
func RejectsInput(err error) bool {
	var status *StatusError
	if !errors.As(err, &status) {
		return false
	}
	switch status.Code {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// Client calls an OpenAI-compatible /v1/embeddings endpoint. With neither APIKey nor
// Credential it is the local llama.cpp sidecar; either one selects a hosted route.
type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	// Credential, when set, is read on every request in place of APIKey, so a key rotated
	// in the cockpit reaches a running client. Setting it marks the route hosted even while
	// it returns "": such a client refuses to embed (ErrNoCredential) rather than send an
	// unauthenticated request to a provider.
	Credential func() string
	Client     *http.Client
	Dimensions int
	BatchSize  int
	Timeout    time.Duration

	limitMu sync.Mutex
	limit   int
}

// embeddingRequest inputs are strings, or token-ID arrays for a local input cut to the limit.
type embeddingRequest struct {
	Input      []any  `json:"input"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     *int      `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed returns one validated vector per input in input order. Every input is first
// fitted to the model's input limit (fit.go), so none can fail the request it rides in.
// Empty input does not issue an HTTP request.
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
	if c.hosted() && c.key() == "" {
		return nil, ErrNoCredential
	}
	dimensions := c.Dimensions
	if dimensions <= 0 {
		dimensions = config.DefaultEmbedDimensions
	}
	batchSize := c.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	limit, err := c.InputLimit(ctx)
	if err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}
	inputs := make([]any, len(texts))
	costs := make([]int, len(texts))
	for index, text := range texts {
		if inputs[index], costs[index], err = c.fitInput(ctx, text, limit); err != nil {
			return nil, fmt.Errorf("embeddings: input %d: %w", index, err)
		}
	}

	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); {
		end := requestEnd(costs, start, batchSize)
		batch, err := c.embedBatch(ctx, inputs[start:end], dimensions)
		if err != nil {
			return nil, fmt.Errorf("embeddings: batch at input %d: %w", start, err)
		}
		out = append(out, batch...)
		start = end
	}
	return out, nil
}

func (c *Client) embedBatch(ctx context.Context, inputs []any, dimensions int) ([][]float64, error) {
	payload := embeddingRequest{Input: inputs, Model: modelOrDefault(c.Model)}
	if c.hosted() {
		// Hosted embedders can MRL-truncate server-side. llama.cpp currently ignores
		// this field, so local responses are narrowed and renormalized below.
		payload.Dimensions = dimensions
	}
	var decoded embeddingResponse
	if err := c.postJSON(ctx, endpoint(c.BaseURL), payload, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) != len(inputs) {
		return nil, fmt.Errorf("endpoint returned %d embeddings for %d inputs", len(decoded.Data), len(inputs))
	}

	out := make([][]float64, len(inputs))
	seen := make([]bool, len(inputs))
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

// postJSON sends one bounded JSON request and decodes a 2xx answer into out. The request
// body never reaches an error: it is document and memory text.
func (c *Client) postJSON(ctx context.Context, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.hosted() {
		req.Header.Set("Authorization", "Bearer "+c.key())
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return &StatusError{Code: resp.StatusCode, Status: resp.Status}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) hosted() bool {
	return c.Credential != nil || strings.TrimSpace(c.APIKey) != ""
}

// key is this request's credential: Credential's current answer, else APIKey.
func (c *Client) key() string {
	if c.Credential != nil {
		return strings.TrimSpace(c.Credential())
	}
	return strings.TrimSpace(c.APIKey)
}

func (c *Client) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: c.requestTimeout()}
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
