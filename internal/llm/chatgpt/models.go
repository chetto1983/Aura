package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type modelWire struct {
	Slug            string   `json:"slug"`
	DisplayName     string   `json:"display_name"`
	Visibility      string   `json:"visibility"`
	ContextWindow   int      `json:"context_window"`
	ContextLength   int      `json:"context_length"`
	InputModalities []string `json:"input_modalities"`
	SupportsImage   bool     `json:"supports_image"`
}

// FetchModels keeps visible account models in the server's preferred picker order.
func FetchModels(ctx context.Context, tokens TokenSource) ([]llm.ModelCatalogEntry, error) {
	token, err := accessToken(ctx, tokens)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return fetchModels(ctx, client, BaseURL, token)
}

func fetchModels(ctx context.Context, client *http.Client, baseURL, token string) ([]llm.ModelCatalogEntry, error) {
	models, err := fetchModelWire(ctx, client, baseURL, token)
	if err != nil {
		return nil, err
	}
	entries := make([]llm.ModelCatalogEntry, 0, len(models))
	// Account catalog differs from API-key catalogs, including its ordered picker:
	// https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference
	for _, model := range models {
		if model.Visibility != "list" || strings.TrimSpace(model.Slug) == "" {
			continue
		}
		window := model.ContextWindow
		if window <= 0 {
			window = model.ContextLength
		}
		entries = append(entries, llm.ModelCatalogEntry{ID: model.Slug, DisplayName: model.DisplayName, ContextWindow: max(window, 0)})
	}
	return entries, nil
}

func fetchModelWire(ctx context.Context, client *http.Client, baseURL, token string) ([]modelWire, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	service := openai.NewModelService(option.WithBaseURL(baseURL), option.WithHTTPClient(client), option.WithAPIKey(token), option.WithMaxRetries(0), option.WithMiddleware(boundedModelsMiddleware))
	page, err := service.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", llm.ErrModelCatalogUnavailable, sanitizeError(err, token))
	}
	var wire struct {
		Models []modelWire `json:"models"`
	}
	if err := json.Unmarshal([]byte(page.RawJSON()), &wire); err != nil {
		return nil, fmt.Errorf("%w: invalid account catalog", llm.ErrModelCatalogUnavailable)
	}
	if wire.Models == nil {
		return nil, fmt.Errorf("%w: account catalog has no models array", llm.ErrModelCatalogUnavailable)
	}
	return wire.Models, nil
}
