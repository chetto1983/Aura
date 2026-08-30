package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrPricingUnavailable marks a rate that could not be read from the provider. The
// caller must degrade to the honest "n/a" (D-23) — never to a fabricated $0, and never
// to a stale hardcoded guess, which is what amendment #93 removed. It is worth a log
// line: something that should have been readable was not.
var ErrPricingUnavailable = errors.New("model pricing unavailable")

// ErrPricingNotApplicable marks a backend that publishes no rates because it charges
// nothing per token — a local llama.cpp/vLLM server. Distinct from Unavailable so a
// local boot does not warn about a catalogue it was never meant to have.
var ErrPricingNotApplicable = errors.New("model pricing not applicable to this backend")

// ErrModelProfileUnavailable marks provider metadata that cannot be resolved for a
// route being prepared. A settings mutation must fail before persistence rather than
// publish a client with stale context or rates from another model.
var ErrModelProfileUnavailable = errors.New("model profile unavailable")

// ModelProfileMetadata is the provider-published portion of an immutable runtime
// model profile. Zero MaxOutputTokens means the provider does not publish that cap.
type ModelProfileMetadata struct {
	ContextWindow   int
	MaxOutputTokens int
	Price           Price
	HasPrice        bool
}

// modelsWire is the GET /models projection. Only consumed fields are declared. The
// rates arrive as JSON STRINGS ("0.00000014"), not numbers, and input_cache_read is
// ABSENT for 155 of the 367 published models — both verified against the live payload
// on 2026-07-29, so neither is a defensive guess.
type modelsWire struct {
	Data []struct {
		ID            string `json:"id"`
		ContextLength int    `json:"context_length"`
		Meta          struct {
			ContextWindow int `json:"n_ctx"`
		} `json:"meta"`
		TopProvider struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		} `json:"top_provider"`
		Pricing struct {
			Prompt         string `json:"prompt"`
			Completion     string `json:"completion"`
			InputCacheRead string `json:"input_cache_read"`
		} `json:"pricing"`
	} `json:"data"`
}

const maxOllamaShowResponseBytes = 1 << 20

type ollamaShowWire struct {
	ModelInfo map[string]json.RawMessage `json:"model_info"`
}

// FetchModelProfile resolves context, output cap and rates from the selected
// provider's OpenAI-compatible /models catalogue. llama.cpp publishes n_ctx under
// data[].meta and no rates; OpenRouter publishes context_length, top_provider and
// pricing. The caller supplies the provider so the same JSON field cannot be
// misinterpreted across protocols.
func FetchModelProfile(
	ctx context.Context,
	client *http.Client,
	provider, baseURL, apiKey, model string,
) (ModelProfileMetadata, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	want := strings.TrimSpace(model)
	catalogueKey := ""
	if provider == "openrouter" {
		want = BaseModelID(want)
		catalogueKey = apiKey
	}
	if want == "" {
		return ModelProfileMetadata{}, fmt.Errorf("%w: empty model id", ErrModelProfileUnavailable)
	}
	if provider == "ollama" {
		metadata, err := fetchOllamaModelProfile(ctx, client, baseURL, want)
		if err != nil {
			return ModelProfileMetadata{}, fmt.Errorf("%w: POST /api/show failed", ErrModelProfileUnavailable)
		}
		return metadata, nil
	}
	wire, err := fetchModels(ctx, client, baseURL, catalogueKey)
	if err != nil {
		return ModelProfileMetadata{}, fmt.Errorf("%w: GET /models failed", ErrModelProfileUnavailable)
	}
	for _, m := range wire.Data {
		if m.ID != want {
			continue
		}
		switch provider {
		case "llamacpp":
			if m.Meta.ContextWindow <= 0 {
				return ModelProfileMetadata{}, fmt.Errorf("%w: %q has no positive meta.n_ctx", ErrModelProfileUnavailable, want)
			}
			return ModelProfileMetadata{ContextWindow: m.Meta.ContextWindow}, nil
		case "openrouter":
			if m.ContextLength <= 0 {
				return ModelProfileMetadata{}, fmt.Errorf("%w: %q has no positive context_length", ErrModelProfileUnavailable, want)
			}
			in, err := ratePer1M(m.Pricing.Prompt)
			if err != nil {
				return ModelProfileMetadata{}, fmt.Errorf("%w: prompt rate for %q: %v", ErrModelProfileUnavailable, want, err)
			}
			out, err := ratePer1M(m.Pricing.Completion)
			if err != nil {
				return ModelProfileMetadata{}, fmt.Errorf("%w: completion rate for %q: %v", ErrModelProfileUnavailable, want, err)
			}
			cache, _ := ratePer1M(m.Pricing.InputCacheRead)
			return ModelProfileMetadata{
				ContextWindow:   m.ContextLength,
				MaxOutputTokens: m.TopProvider.MaxCompletionTokens,
				Price: Price{
					InputPer1M: in, OutputPer1M: out, CacheReadPer1M: cache,
				},
				HasPrice: true,
			}, nil
		default:
			return ModelProfileMetadata{}, fmt.Errorf("%w: unsupported provider %q", ErrModelProfileUnavailable, provider)
		}
	}
	return ModelProfileMetadata{}, fmt.Errorf("%w: %q not among the %d published models", ErrModelProfileUnavailable, want, len(wire.Data))
}

func fetchOllamaModelProfile(
	ctx context.Context, client *http.Client, baseURL, model string,
) (ModelProfileMetadata, error) {
	showURL, err := ollamaShowURL(baseURL)
	if err != nil {
		return ModelProfileMetadata{}, err
	}
	body, err := json.Marshal(struct {
		Model string `json:"model"`
	}{Model: model})
	if err != nil {
		return ModelProfileMetadata{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, showURL, bytes.NewReader(body))
	if err != nil {
		return ModelProfileMetadata{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ModelProfileMetadata{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response
	if resp.StatusCode != http.StatusOK {
		return ModelProfileMetadata{}, fmt.Errorf("POST /api/show returned %d", resp.StatusCode)
	}
	var wire ollamaShowWire
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOllamaShowResponseBytes)).Decode(&wire); err != nil {
		return ModelProfileMetadata{}, fmt.Errorf("decode /api/show: %w", err)
	}
	contextValues := make([]json.RawMessage, 0, 1)
	for key, value := range wire.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			contextValues = append(contextValues, value)
		}
	}
	if len(contextValues) != 1 {
		return ModelProfileMetadata{}, fmt.Errorf("/api/show has %d context_length fields", len(contextValues))
	}
	var contextWindow int
	if err := json.Unmarshal(contextValues[0], &contextWindow); err != nil || contextWindow <= 0 {
		return ModelProfileMetadata{}, errors.New("/api/show context_length is not a positive integer")
	}
	return ModelProfileMetadata{ContextWindow: contextWindow}, nil
}

func ollamaShowURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid Ollama base URL")
	}
	if strings.TrimRight(parsed.Path, "/") != "/v1" {
		return "", errors.New("ollama base URL must end in /v1")
	}
	parsed.Path = "/api/show"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func fetchModels(ctx context.Context, client *http.Client, baseURL, apiKey string) (modelsWire, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return modelsWire{}, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return modelsWire{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response
	if resp.StatusCode != http.StatusOK {
		return modelsWire{}, fmt.Errorf("GET /models returned %d", resp.StatusCode)
	}
	var wire modelsWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return modelsWire{}, fmt.Errorf("decode /models: %w", err)
	}
	return wire, nil
}

// BaseModelID strips the routing-variant suffix. `deepseek/deepseek-v4-flash:nitro` is
// what an operator configures and what the request sends, but NONE of the 367 published
// ids carry a `:` variant — the catalogue only ever lists the base model, so a lookup
// that skips this step always misses. Vendor and name never contain a colon.
func BaseModelID(model string) string {
	base, _, _ := strings.Cut(model, ":")
	return base
}

// FetchModelPrice reads the published rate for ONE model — the configured one. Aura
// uses a single model per process, so pulling and caching the whole 367-entry, ~600 KB
// catalogue would be waste; this selects during the decode pass.
//
// GET /models needs NO credential (verified: HTTP 200 with no Authorization header and
// again with a garbage bearer), so an empty apiKey is not an error here. The key is sent
// when present only because the endpoint accepts it.
func FetchModelPrice(ctx context.Context, client *http.Client, baseURL, apiKey, model string) (Price, error) {
	want := BaseModelID(model)
	if want == "" {
		return Price{}, fmt.Errorf("%w: empty model id", ErrPricingUnavailable)
	}

	wire, err := fetchModels(ctx, client, baseURL, apiKey)
	if err != nil {
		return Price{}, fmt.Errorf("%w: %w", ErrPricingUnavailable, err)
	}

	for _, m := range wire.Data {
		if m.ID != want {
			continue
		}
		in, err := ratePer1M(m.Pricing.Prompt)
		if err != nil {
			return Price{}, fmt.Errorf("%w: prompt rate for %q: %w", ErrPricingUnavailable, want, err)
		}
		out, err := ratePer1M(m.Pricing.Completion)
		if err != nil {
			return Price{}, fmt.Errorf("%w: completion rate for %q: %w", ErrPricingUnavailable, want, err)
		}
		// Absent for 155 models; a zero CacheReadPer1M means "bill cache reads at the
		// prompt rate", which is what CostUSDValue does.
		cache, _ := ratePer1M(m.Pricing.InputCacheRead)
		return Price{InputPer1M: in, OutputPer1M: out, CacheReadPer1M: cache}, nil
	}
	return Price{}, fmt.Errorf("%w: %q not among the %d published models", ErrPricingUnavailable, want, len(wire.Data))
}

// ResolveModelProfile atomically replaces the provider-derived fields in c. An
// operator-pinned context/output value remains authoritative; discovered metadata
// fills only defaults. Local rates are an explicit zero entry so included compute is
// distinguishable from an unresolved cloud rate at every CostUSD call site.
func (c *Config) ResolveModelProfile(ctx context.Context) error {
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	metadata, err := FetchModelProfile(ctx, client, c.Provider, c.BaseURL, c.APIKey, c.Model)
	if err != nil {
		return err
	}
	candidate := *c
	candidate.Headers = maps.Clone(c.Headers)
	candidate.Prices = maps.Clone(c.Prices)
	if candidate.Prices == nil {
		candidate.Prices = map[string]Price{}
	}
	if !candidate.ContextWindowConfigured {
		candidate.ContextWindow = metadata.ContextWindow
	}
	if !candidate.MaxOutputTokensConfigured && metadata.MaxOutputTokens > 0 {
		candidate.MaxOutputTokens = metadata.MaxOutputTokens
	}
	switch strings.ToLower(strings.TrimSpace(c.Provider)) {
	case "llamacpp":
		candidate.Prices[c.Model] = Price{}
		candidate.CostStatus = CostStatusLocalIncluded
	case "ollama":
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(c.Model)), "-cloud") {
			delete(candidate.Prices, c.Model)
			candidate.CostStatus = CostStatusSubscriptionIncluded
		} else {
			candidate.Prices[c.Model] = Price{}
			candidate.CostStatus = CostStatusLocalIncluded
		}
	case "openrouter":
		candidate.CostStatus = CostStatusUnknown
		if !metadata.HasPrice {
			break
		}
		if _, pinned := candidate.Prices[c.Model]; !pinned {
			candidate.Prices[c.Model] = metadata.Price
		}
		candidate.CostStatus = CostStatusRateEstimate
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrModelProfileUnavailable, err)
	}
	*c = candidate
	return nil
}

// ratePer1M converts a per-token rate string to USD per 1,000,000 tokens.
func ratePer1M(s string) (float64, error) {
	if strings.TrimSpace(s) == "" {
		return 0, errors.New("empty rate")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return v * 1_000_000, nil
}

// ResolvePricing fills c.Prices for the configured model from the live catalogue. It is
// called explicitly by the composition root rather than from Load() so that constructing
// a Config never touches the network (tests, `aura config show`, offline boots).
//
// A local backend has no priced catalogue and genuinely costs nothing per token, so it
// returns ErrPricingNotApplicable rather than looking like a failed cloud lookup.
func (c *Config) ResolvePricing(ctx context.Context) error {
	if ReasoningTarget(c.Provider, c.BaseURL) == ReasoningTargetLlamaCpp ||
		strings.EqualFold(strings.TrimSpace(c.Provider), "ollama") {
		return fmt.Errorf("%w: local backend publishes no rates", ErrPricingNotApplicable)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	p, err := FetchModelPrice(ctx, client, c.BaseURL, c.APIKey, c.Model)
	if err != nil {
		return err
	}
	if c.Prices == nil {
		c.Prices = map[string]Price{}
	}
	// Keyed on the CONFIGURED id, because that is the string every CostUSD call site
	// passes (cfg.Model) — not the base id the catalogue publishes.
	if _, pinned := c.Prices[c.Model]; !pinned {
		c.Prices[c.Model] = p
	}
	return nil
}
