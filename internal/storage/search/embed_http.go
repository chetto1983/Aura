package search

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

	"github.com/aura/aura/internal/httputil"
	"github.com/aura/aura/internal/stringx"
)

// NewOpenAICompatEmbeddingFunction is the single-vector counterpart of
// NewOpenAICompatBatchEmbeddingFunction. Both call /v1/embeddings on an
// OpenAI-compatible host (Mistral, OpenAI, llama.cpp server, etc.). The
// single-vector form is what most of Aura uses for query embeddings; the
// batch form is used during wiki rebuilds.
//
// When normalize is true the returned vector is L2-normalized so cosine
// similarity == dot product. Qdrant's cosine distance benefits from
// normalized vectors (skips the per-query magnitude division).
//
// outputDim activates MRL truncation: when > 0 AND smaller than the model's
// native dim, the response vector is truncated to the first outputDim
// components and re-normalized (renorm is mandatory after MRL truncation
// because the prefix's L2 norm is not 1.0 in general). When 0 the vector
// is returned at the model's native dim and the `normalize` flag governs
// L2-normalization. Truncation only makes sense for MRL-trained models
// such as embeddinggemma-300m; applying it to Mistral-embed (non-MRL)
// would throw away signal without preserving rank.
func NewOpenAICompatEmbeddingFunction(baseURL, apiKey, model string, outputDim int, normalize bool, client *http.Client) EmbeddingFunc {
	baseURL = stringx.NormalizeBaseURL(baseURL)
	model = strings.TrimSpace(model)
	if baseURL == "" || apiKey == "" || model == "" {
		return nil
	}
	if client == nil {
		client = httputil.NewHTTPClient(0, 2*time.Minute)
	}
	e := &openAICompatEmbedder{baseURL: baseURL, apiKey: apiKey, model: model, http: client, outputDim: outputDim, normalize: normalize}
	return e.Embed
}

type openAICompatEmbedder struct {
	baseURL   string
	apiKey    string
	model     string
	outputDim int
	normalize bool
	http      *http.Client
}

func (e *openAICompatEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("embedder unavailable")
	}
	body, err := json.Marshal(map[string]any{
		"model": e.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if readErr != nil {
		return nil, fmt.Errorf("embeddings read response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embeddings returned %s: %s", resp.Status, strings.TrimSpace(string(respBytes)))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("embeddings decode response: %w", err)
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings returned empty vector")
	}
	vec := out.Data[0].Embedding
	if e.outputDim > 0 && e.outputDim < len(vec) {
		// MRL truncation: slice to first outputDim components, then
		// re-normalize because the prefix's L2 norm is not 1.0 in general.
		// Renormalization is mandatory after MRL truncation regardless of
		// the normalize flag — Qdrant cosine distance requires unit vectors
		// for the truncated dim to compare against indexed vectors of the
		// same truncated dim.
		vec = l2Normalize(vec[:e.outputDim])
	} else if e.normalize {
		vec = l2Normalize(vec)
	}
	return vec, nil
}

func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1.0 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}
