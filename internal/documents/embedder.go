package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/knowledge"
)

// EmbeddingGenerator generates vector embeddings for document chunk text.
type EmbeddingGenerator interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// EmbeddingClient calls Aura's OpenAI-compatible local embedding sidecar.
type EmbeddingClient struct {
	BaseURL    string
	Model      string
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
	body, err := json.Marshal(map[string]any{
		"model": inputModel(c.Model),
		"input": texts,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
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
		if len(item.Embedding) != dim {
			return nil, fmt.Errorf("embedding %d has dimension %d, want %d", i, len(item.Embedding), dim)
		}
		out = append(out, item.Embedding)
	}
	return out, nil
}

func inputModel(model string) string {
	if model == "" {
		return "aura-local-embedding"
	}
	return model
}
