package llm

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/tracing"
)

// OllamaClient implements Client for local Ollama instances.
// It uses the same OpenAI-compatible API since Ollama exposes one.
type OllamaClient struct {
	inner *OpenAIClient
}

// OllamaConfig holds configuration for an Ollama client.
type OllamaConfig struct {
	BaseURL string // e.g. "http://localhost:11434/v1"
	Model   string
}

// NewOllamaClient creates a new client for a local Ollama instance.
// Ollama exposes an OpenAI-compatible API, so we reuse OpenAIClient.
func NewOllamaClient(cfg OllamaConfig) *OllamaClient {
	inner := NewOpenAIClient(OpenAIConfig{
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		// Ollama doesn't require an API key for local usage
		APIKey: "ollama",
	})
	return &OllamaClient{inner: inner}
}

// Send forwards to the OpenAI-compatible Ollama API.
func (c *OllamaClient) Send(ctx context.Context, req Request) (Response, error) {
	return c.inner.Send(ctx, req)
}

// Stream forwards to the OpenAI-compatible Ollama API.
func (c *OllamaClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	return c.inner.Stream(ctx, req)
}

// FailoverClient tries multiple LLM providers in order, falling back on error.
type FailoverClient struct {
	providers []Client
	names     []string
}

// NewFailoverClient creates a client that tries providers in order.
// The first successful response wins; errors trigger fallback to the next provider.
func NewFailoverClient(providers []Client, names []string) (*FailoverClient, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}
	if len(names) < len(providers) {
		// Pad names with generic labels
		for i := len(names); i < len(providers); i++ {
			names = append(names, fmt.Sprintf("provider_%d", i))
		}
	}
	return &FailoverClient{providers: providers, names: names}, nil
}

// Send tries each provider in order until one succeeds.
func (f *FailoverClient) Send(ctx context.Context, req Request) (Response, error) {
	ctx, span := tracing.StartSpan(ctx, "llm", "failover.send")
	defer span.End()
	var lastErr error
	for i, provider := range f.providers {
		// Check context before attempting each provider
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		resp, err := provider.Send(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = fmt.Errorf("%s: %w", f.names[i], err)
	}
	return Response{}, fmt.Errorf("all providers failed: %w", lastErr)
}

// Stream tries each provider in order until one succeeds.
func (f *FailoverClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	ctx, span := tracing.StartSpan(ctx, "llm", "failover.stream")
	defer span.End()
	var lastErr error
	for i, provider := range f.providers {
		ch, err := provider.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		lastErr = fmt.Errorf("%s: %w", f.names[i], err)
	}
	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}
