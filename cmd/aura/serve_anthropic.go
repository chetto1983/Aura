package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/anthropic"
	"github.com/chetto1983/aura/internal/redact"
)

const (
	// anthropicDefaultMaxTokens replaces a MaxTokens below the floor (plan decision 4).
	anthropicDefaultMaxTokens = 32_768
	// anthropicWorkingWindow is the window used when the price has no long-prompt
	// threshold (plan decision 5): a 1M window would let compaction wait for ~500k prompts.
	anthropicWorkingWindow = 200_000
)

// The account catalogue and the price list are package variables so the route's save and
// boot paths are testable without reaching Anthropic or OpenRouter.
var (
	fetchAnthropicModels     = anthropic.FetchModels
	anthropicPriceCatalogURL = llm.DefaultBaseURL
)

// anthropicRouteClient turns a missing house key into the same structured refusal every
// unconfigured route answers with, before any dial.
type anthropicRouteClient struct{ inner *anthropic.Client }

func (c anthropicRouteClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	stream, err := c.inner.Stream(ctx, req)
	if errors.Is(err, anthropic.ErrNoKey) {
		return nil, llmNotConfiguredError{}
	}
	return stream, err
}

// bootAnthropicProfile resolves the Claude model's limits and price at boot, fail-soft
// like ChatGPT's: an unreachable catalogue leaves the configured fallback in place.
func bootAnthropicProfile(ctx context.Context, cfg *llm.Config, key anthropic.KeySource) {
	if err := validateAnthropicModel(ctx, cfg, key); err != nil {
		slog.Warn("Claude model profile unresolved; using configured fallback", "model", redact.Line(cfg.Model), "err", err)
	}
}

func validateAnthropicModel(ctx context.Context, cfg *llm.Config, key anthropic.KeySource) error {
	if cfg.Provider != llm.AnthropicProvider {
		return nil
	}
	models, err := fetchAnthropicModels(ctx, key)
	if errors.Is(err, anthropic.ErrNoKey) {
		return errors.New("add the Claude Console API key in Settings before selecting a Claude model")
	}
	if err != nil {
		return fmt.Errorf("unable to load your Claude models: %w", err)
	}
	price, priced := claudeListPrice(ctx, cfg.Model)
	return applyAnthropicCatalogModel(cfg, models, price, priced)
}

// claudeListPrice reads the model's list price from OpenRouter's public catalogue, with
// no key: Anthropic publishes no price endpoint. A miss leaves the cost unknown.
func claudeListPrice(ctx context.Context, model string) (llm.Price, bool) {
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	metadata, err := llm.FetchModelProfile(ctx, client, "openrouter", anthropicPriceCatalogURL, "", anthropic.OpenRouterID(model))
	if err != nil || !metadata.HasPrice {
		slog.Warn("Claude list price unavailable; its cost stays unknown", "model", redact.Line(model), "err", err)
		return llm.Price{}, false
	}
	return metadata.Price, true
}

// applyAnthropicCatalogModel fills the route's profile from the account's entry for the
// model. MaxTokens has no pin flag, so a value below the client's floor (the generic
// 4,096 default included) becomes 32,768 and one at the floor or above is the operator's.
// The output cap follows MaxTokens so the window reserves what the client sends.
func applyAnthropicCatalogModel(cfg *llm.Config, models []llm.ModelCatalogEntry, price llm.Price, priced bool) error {
	index := slices.IndexFunc(models, func(m llm.ModelCatalogEntry) bool { return m.ID == cfg.Model })
	if index < 0 {
		return errors.New("select a model available to your Claude account")
	}
	model := models[index]
	candidate := *cfg
	candidate.Prices = maps.Clone(cfg.Prices)
	if candidate.Prices == nil {
		candidate.Prices = map[string]llm.Price{}
	}
	if _, pinned := candidate.Prices[model.ID]; priced && !pinned {
		candidate.Prices[model.ID] = price
	}
	effective, hasPrice := candidate.Prices[model.ID]
	if hasPrice {
		candidate.CostStatus = llm.CostStatusRateEstimate
	}
	if !candidate.ContextWindowConfigured {
		candidate.ContextWindow = anthropicWindow(effective, model.ContextWindow)
	}
	if err := applyAnthropicTokenLimits(&candidate, model.MaxOutputTokens); err != nil {
		return err
	}
	candidate.SupportedReasoningEfforts = slices.Clone(model.SupportedReasoningEfforts)
	candidate.ReasoningMandatory = model.ReasoningMandatory
	if err := candidate.Validate(); err != nil {
		return err
	}
	*cfg = candidate
	return nil
}

// applyAnthropicTokenLimits sets MaxTokens and the output cap. A MaxTokens below the
// floor is not the operator's, so it takes the default, held to the effective cap (the
// pinned one, else the model's own max_tokens); a cap below the floor cannot carry
// adaptive thinking and is refused by name.
func applyAnthropicTokenLimits(cfg *llm.Config, modelMaxOutput int) error {
	if cfg.MaxTokens < anthropic.MinMaxTokens {
		capTokens, capSource := modelMaxOutput, "from the model's catalogue"
		if cfg.MaxOutputTokensConfigured {
			capTokens, capSource = cfg.MaxOutputTokens, "pinned"
		}
		cfg.MaxTokens = anthropicDefaultMaxTokens
		if capTokens > 0 {
			if capTokens < anthropic.MinMaxTokens {
				return fmt.Errorf("max output tokens %s at %d is below the Claude route's %d floor",
					capSource, capTokens, anthropic.MinMaxTokens)
			}
			cfg.MaxTokens = min(anthropicDefaultMaxTokens, capTokens)
		}
	}
	if !cfg.MaxOutputTokensConfigured {
		cfg.MaxOutputTokens = cfg.MaxTokens
		if modelMaxOutput > 0 {
			cfg.MaxOutputTokens = min(modelMaxOutput, cfg.MaxTokens)
		}
	}
	return nil
}

// anthropicWindow is the long-prompt threshold when the price has one, else the working
// window, never above what the model accepts.
func anthropicWindow(price llm.Price, maxInput int) int {
	window := anthropicWorkingWindow
	if price.LongPrompt != nil && price.LongPrompt.MinPromptTokens > 0 {
		window = price.LongPrompt.MinPromptTokens
	}
	if maxInput > 0 {
		window = min(window, maxInput)
	}
	return window
}
