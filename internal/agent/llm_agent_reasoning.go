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
	return prompt.NewReasoningClassifier(cfg.Embedder, cfg.ExampleStore)
}

func (a *LlmAgent) adaptiveReasoningTier(ctx context.Context) (prompt.ReasoningTier, bool) {
	if !a.cfg.AdaptiveReasoning || !prompt.IsOpenRouterReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) {
		return "", false
	}
	user := prompt.LastGenuineUserContent(a.history)
	if strings.TrimSpace(user) == "" {
		return prompt.ReasoningTierLow, true
	}

	// Fast path: the local embedding classifier (granite sidecar, ~10ms) replaces
	// the per-turn LLM router round-trip. On any embed failure it returns false
	// and we fall through to the LLM router below (graceful degradation).
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
			"fallback":  "llm_router",
		})
	}

	routeCtx, cancel := context.WithTimeout(ctx, a.reasoningRouterTimeout())
	defer cancel()
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

	started := time.Now()
	ch, err := a.streamWithOpenRetry(routeCtx, req, "adaptive_reasoning_router")
	if err != nil {
		recordLLMDuration(time.Since(started))
		recordLLMError(llmErrorKind("reasoning_router_open", err))
		reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": err.Error(), "fallback_tier": prompt.ReasoningTierLow})
		return prompt.ReasoningTierLow, true
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			recordLLMDuration(time.Since(started))
			recordLLMError(llmErrorKind("reasoning_router_stream", c.Err))
			reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": c.Err.Error(), "fallback_tier": prompt.ReasoningTierLow})
			return prompt.ReasoningTierLow, true
		}
		if c.Usage != nil {
			recordUsage(*c.Usage)
		}
		b.WriteString(c.Text)
	}
	recordLLMDuration(time.Since(started))
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
	total := time.Duration(a.cfg.TotalTimeoutSec) * time.Second
	if total <= 0 {
		return 8 * time.Second
	}
	if total < 8*time.Second {
		return total
	}
	return 8 * time.Second
}
