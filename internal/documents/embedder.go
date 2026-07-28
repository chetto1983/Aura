package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/knowledge"
)

// EmbeddingGenerator generates vector embeddings for document chunk text.
type EmbeddingGenerator interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// EmbeddingClient calls Aura's OpenAI-compatible embedding endpoint — the local
// llama.cpp sidecar by default, or an OpenRouter cloud embedder when APIKey is set
// (the config-only local↔cloud swap; resolve via config.EmbedRoute). APIKey is
// set ONLY on the Authorization header at request-build time, never logged.
type EmbeddingClient struct {
	BaseURL    string
	Model      string
	APIKey     string
	Client     *http.Client
	Dimensions int
}

// Embed generates embeddings for texts and validates the configured dimensions.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, fmt.Errorf("embedding base URL is empty")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	payload := map[string]any{
		"model": inputModel(c.Model),
		"input": texts,
	}
	if c.APIKey != "" && c.Dimensions > 0 {
		// Cloud embedders Matryoshka-truncate server-side to the requested width —
		// the SINGLE AURA_EMBED_DIMENSIONS knob. The local llama.cpp sidecar ignores
		// this parameter entirely, so the request omits it there and truncateMRL
		// narrows the response instead.
		payload["dimensions"] = c.Dimensions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey) // cloud embed route only (D-28)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embedding sidecar: HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("embedding sidecar: decode: %w", err)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding sidecar returned %d embeddings for %d inputs", len(decoded.Data), len(texts))
	}
	dim := c.Dimensions
	if dim <= 0 {
		dim = knowledge.DefaultEmbedDimensions
	}
	out := make([][]float64, 0, len(decoded.Data))
	for i, item := range decoded.Data {
		vec, err := truncateMRL(item.Embedding, dim)
		if err != nil {
			return nil, fmt.Errorf("embedding %d: %w", i, err)
		}
		out = append(out, vec)
	}
	return out, nil
}

// truncateMRL narrows a wider embedding to the requested width using Matryoshka
// Representation Learning: keep the leading dim components, renormalise to unit
// length. The width is never decided here — it is c.Dimensions, i.e. whatever
// AURA_EMBED_DIMENSIONS says the vector index was built for.
//
// MRL-trained embedders publish a native width larger than the vector index expects
// and are explicitly trained so that a leading slice is itself a valid embedding. The truncation has to happen here because the
// llama.cpp sidecar ignores the OpenAI `dimensions` request parameter — verified:
// asking for 256 returns the model's native width unchanged — so nothing upstream
// can narrow it for us. Renormalising matters: a truncated slice is no longer unit
// length, and cosine over unnormalised vectors would silently skew every score.
//
// A shorter-than-contract embedding is an error, not something to pad: it means the
// sidecar is serving a different model than the index was built for.
func truncateMRL(vec []float64, dim int) ([]float64, error) {
	if len(vec) == dim {
		return vec, nil
	}
	if len(vec) < dim {
		return nil, fmt.Errorf("has dimension %d, want %d", len(vec), dim)
	}
	out := make([]float64, dim)
	copy(out, vec[:dim])
	var sum float64
	for _, v := range out {
		sum += v * v
	}
	if sum == 0 {
		return nil, fmt.Errorf("truncated to %d components that are all zero", dim)
	}
	norm := math.Sqrt(sum)
	for i := range out {
		out[i] /= norm
	}
	return out, nil
}

func inputModel(model string) string {
	if model == "" {
		return "aura-local-embedding"
	}
	return model
}
