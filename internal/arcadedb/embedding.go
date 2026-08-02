package arcadedb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Embedder turns text into vectors. It is an interface because the memory client
// must work without one: with no embedder the retrieval is the lexical leg alone,
// which is the behaviour that shipped and must not regress if the sidecar is down.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// EmbeddingGemma is trained with ASYMMETRIC task prefixes and llama.cpp does not
// add them, so every caller must. The two literals below are the model card's own,
// trailing space included — it is part of the string, not formatting:
//
//	https://ai.google.dev/gemma/docs/embeddinggemma/model_card
//	Retrieval (Query)    -> "task: search result | query: {content}"
//	Retrieval (Document) -> "title: {title | \"none\"} | text: {content}"
//
// A fact statement is the DOCUMENT and a search string is the QUERY; the pairing
// is directional and swapping it measurably costs recall. These are retrieval
// prefixes only — a symmetric job (comparing two utterances, as the reasoning-tier
// classifier does) wants "task: sentence similarity | query: " or nothing at all,
// never these.
const (
	taskQueryPrefix    = "task: search result | query: "
	taskDocumentPrefix = "title: none | text: "
)

// withTask prepends a task prefix to every text, returning a new slice so the
// caller's input is never mutated.
func withTask(prefix string, texts []string) []string {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = prefix + t
	}
	return out
}

// SidecarEmbedder calls an OpenAI-compatible /v1/embeddings endpoint — Aura's
// llama.cpp sidecar, or any hosted embedder that speaks the same shape.
type SidecarEmbedder struct {
	BaseURL string
	Model   string
	APIKey  string
	Client  *http.Client
}

// NewSidecarEmbedder builds an embedder against baseURL. An empty baseURL yields
// nil, so a caller can pass the configured value straight through and get the
// lexical-only path when nothing is configured, rather than a client that fails
// on every call.
func NewSidecarEmbedder(baseURL, model, apiKey string, timeout time.Duration) *SidecarEmbedder {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &SidecarEmbedder{
		BaseURL: baseURL,
		Model:   strings.TrimSpace(model),
		APIKey:  strings.TrimSpace(apiKey),
		Client:  &http.Client{Timeout: timeout},
	}
}

type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed returns one vector per input, in input order.
func (e *SidecarEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if e == nil {
		return nil, fmt.Errorf("arcadedb: no embedder configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embeddingRequest{Input: texts, Model: e.Model})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: encode embedding request: %w", err)
	}
	endpoint := e.BaseURL
	if !strings.HasSuffix(endpoint, "/embeddings") {
		endpoint = strings.TrimSuffix(endpoint, "/v1") + "/v1/embeddings"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: embed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("arcadedb: embed: sidecar returned %s", resp.Status)
	}
	var decoded embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("arcadedb: decode embedding response: %w", err)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("arcadedb: embed: asked for %d vectors, got %d", len(texts), len(decoded.Data))
	}
	// The endpoint is documented to echo the request order, but it also carries an
	// explicit index; honouring it costs nothing and a silent misalignment here
	// would attach every fact to the wrong vector.
	out := make([][]float64, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(out) {
			return nil, fmt.Errorf("arcadedb: embed: response index %d out of range", item.Index)
		}
		out[item.Index] = item.Embedding
	}
	for i, vector := range out {
		if len(vector) == 0 {
			return nil, fmt.Errorf("arcadedb: embed: no vector for input %d", i)
		}
	}
	return out, nil
}
