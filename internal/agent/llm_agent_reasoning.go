package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

const reasoningRouterSystemPrompt = "Classify the latest user request into one reasoning tier for the next assistant turn. Reply only JSON: {\"tier\":\"none\"|\"low\"|\"high\"}. none=greeting/simple stable fact/direct short transform. low=current web/news/weather/prices/schedules/lookups or small tool use. high=coding/debugging/design/proofs/scraping/multi-step analysis."

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
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: reasoningRouterSystemPrompt}, {Role: llm.RoleUser, Content: user}},
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
		reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": err.Error(), "fallback_tier": prompt.ReasoningTierLow})
		return prompt.ReasoningTierLow, true
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": c.Err.Error(), "fallback_tier": prompt.ReasoningTierLow})
			return prompt.ReasoningTierLow, true
		}
		b.WriteString(c.Text)
	}
	raw := strings.TrimSpace(b.String())
	tier := parseReasoningRouterTier(raw)
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

func parseReasoningRouterTier(raw string) prompt.ReasoningTier {
	var obj struct {
		Tier string `json:"tier"`
	}
	body := strings.TrimSpace(raw)
	if start := strings.Index(body, "{"); start >= 0 {
		if end := strings.LastIndex(body, "}"); end >= start {
			body = body[start : end+1]
		}
	}
	if err := json.Unmarshal([]byte(body), &obj); err == nil {
		return normalizeReasoningTier(obj.Tier)
	}
	return normalizeReasoningTier(raw)
}

func normalizeReasoningTier(s string) prompt.ReasoningTier {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "` \t\r\n\"'")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimSpace(s)
	switch s {
	case "none", "no", "off", "false":
		return prompt.ReasoningTierNone
	case "low", "small", "light":
		return prompt.ReasoningTierLow
	case "high", "deep":
		return prompt.ReasoningTierHigh
	default:
		return ""
	}
}
