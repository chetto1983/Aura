package agent

import (
	"context"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// resolveClassifier prefers the shared injected classifier (production, anchors
// built once); it falls back to a per-agent one built from Embedder when only
// that is supplied (tests/standalone). nil when neither is wired.
func resolveClassifier(cfg LlmAgentConfig) *prompt.ReasoningClassifier {
	if cfg.Classifier != nil {
		return cfg.Classifier
	}
	return prompt.NewReasoningClassifier(cfg.Embedder)
}

func (a *LlmAgent) adaptiveReasoningTier(ctx context.Context) (prompt.ReasoningTier, bool) {
	if !a.cfg.AdaptiveReasoning || !prompt.IsOpenRouterReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) {
		return "", false
	}
	user := prompt.LastGenuineUserContent(a.history)
	if strings.TrimSpace(user) == "" {
		return prompt.ReasoningTierLow, true
	}

	// Fast path: the local embedding classifier (embedding sidecar, ~10ms) replaces
	// the per-turn LLM router round-trip. On any embed failure it returns false;
	// when a classifier is wired, degrade to static low reasoning instead of
	// spending a second network call every turn.
	if a.classifier != nil {
		if tier, ok := a.classifier.Classify(ctx, user); ok {
			reasoningtrace.Record("adaptive_reasoning_classifier_decision", map[string]any{
				"thread_id": a.sessionID,
				"tier":      tier,
				"source":    "embedding",
			})
			return tier, true
		}
		reasoningtrace.Record("adaptive_reasoning_classifier_miss", map[string]any{
			"thread_id": a.sessionID,
			"fallback":  "static_low",
		})
		return prompt.ReasoningTierLow, true
	}

	routeCtx, cancel := context.WithTimeout(ctx, a.reasoningRouterTimeout())
	defer cancel()
	routeCtx, llmEnd := llmCallBoundary.Start(routeCtx)
	var boundaryErr error
	defer llmEnd.PanicSafe(&boundaryErr)
	enabled := false
	req := llm.Request{
		Model:       a.cfg.Model,
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: prompt.ReasoningRouterSystemPrompt}, {Role: llm.RoleUser, Content: user}},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &enabled},
		SessionID:   a.sessionID,
		ToolChoice:  "none",
	}
	reasoningtrace.Record("adaptive_reasoning_router_request", map[string]any{
		"thread_id":  a.sessionID,
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"reasoning":  req.Reasoning,
		"user":       user,
	})

	ch, err := a.streamWithOpenRetry(routeCtx, req, "adaptive_reasoning_router")
	if err != nil {
		boundaryErr = err
		recordLLMError(llmErrorKind("reasoning_router_open", err))
		reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": err.Error(), "fallback_tier": prompt.ReasoningTierLow})
		return prompt.ReasoningTierLow, true
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			boundaryErr = c.Err
			recordLLMError(llmErrorKind("reasoning_router_stream", c.Err))
			reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": c.Err.Error(), "fallback_tier": prompt.ReasoningTierLow})
			return prompt.ReasoningTierLow, true
		}
		if c.Usage != nil {
			recordUsage(*c.Usage)
		}
		b.WriteString(c.Text)
	}
	raw := strings.TrimSpace(b.String())
	tier := prompt.ParseReasoningRouterTier(raw)
	if !tier.Valid() {
		reasoningtrace.Record("adaptive_reasoning_router_invalid", map[string]any{"raw": raw, "fallback_tier": prompt.ReasoningTierLow})
		return prompt.ReasoningTierLow, true
	}
	reasoningtrace.Record("adaptive_reasoning_router_decision", map[string]any{"raw": raw, "tier": tier})
	return tier, true
}

func (a *LlmAgent) reasoningRouterTimeout() time.Duration {
	const maxReasoningRouterTimeout = 2 * time.Second
	total := time.Duration(a.cfg.TotalTimeoutSec) * time.Second
	if total <= 0 {
		return maxReasoningRouterTimeout
	}
	if total < maxReasoningRouterTimeout {
		return total
	}
	return maxReasoningRouterTimeout
}
