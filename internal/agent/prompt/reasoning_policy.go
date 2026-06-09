package prompt

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

const (
	noReasoningMaxTokens    = 512
	smallReasoningMaxTokens = 2048
)

// ReasoningTier is the small provider-neutral routing result the runtime applies
// to the main request after an external policy/router has classified the turn.
type ReasoningTier string

const (
	// ReasoningTierNone asks the provider to avoid reasoning tokens on simple turns.
	ReasoningTierNone ReasoningTier = "none"
	// ReasoningTierLow requests a small reasoning budget for lookup/tool turns.
	ReasoningTierLow ReasoningTier = "low"
	// ReasoningTierHigh requests a deeper reasoning budget for complex work.
	ReasoningTierHigh ReasoningTier = "high"
)

// Valid reports whether t is one of the supported routing tiers.
func (t ReasoningTier) Valid() bool {
	switch t {
	case ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh:
		return true
	default:
		return false
	}
}

// ApplyAdaptiveReasoning applies a precomputed tier to the main request. It never
// forces ToolChoice="none": the hot manifest keeps active tools and deferred tool
// names visible, so the model can still call tool_search when needed.
func ApplyAdaptiveReasoning(req *llm.Request, provider string, cfg llm.Config, tier ReasoningTier) {
	if !cfg.AdaptiveReasoning || !IsOpenRouterReasoningTarget(provider, cfg.BaseURL) || !tier.Valid() {
		return
	}

	req.Reasoning = tier.reasoning()
	req.MaxTokens = tier.maxTokens(cfg.MaxTokens)
}

// IsOpenRouterReasoningTarget reports whether provider/baseURL support this
// OpenRouter reasoning projection.
func IsOpenRouterReasoningTarget(provider, baseURL string) bool {
	if !strings.EqualFold(provider, "openrouter") {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(baseURL))
	return base == "" || strings.Contains(base, "openrouter.ai")
}

func (t ReasoningTier) reasoning() llm.ReasoningConfig {
	switch t {
	case ReasoningTierHigh:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh, Exclude: boolPtr(true)}
	case ReasoningTierLow:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortLow, Exclude: boolPtr(true)}
	default:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortNone, Exclude: boolPtr(true)}
	}
}

func (t ReasoningTier) maxTokens(configuredMax int) int {
	switch t {
	case ReasoningTierHigh:
		return configuredOrDefault(configuredMax)
	case ReasoningTierLow:
		return cappedTokens(smallReasoningMaxTokens, configuredMax)
	default:
		return cappedTokens(noReasoningMaxTokens, configuredMax)
	}
}

// LastGenuineUserContent returns the newest real user request, skipping synthetic
// budget, workspace, time, recovery, and completion-check nudges.
func LastGenuineUserContent(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != llm.RoleUser || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if isSyntheticUserHint(m.Content) {
			continue
		}
		return m.Content
	}
	return ""
}

func isSyntheticUserHint(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "<budget>") ||
		strings.HasPrefix(trimmed, "<workspace>") ||
		strings.HasPrefix(trimmed, "<current_time>") ||
		strings.HasPrefix(trimmed, "<today>") ||
		strings.HasPrefix(trimmed, "Stop calling tools.") ||
		strings.HasPrefix(trimmed, "You have run out of tool-call budget") ||
		strings.HasPrefix(trimmed, "You have already called `") ||
		strings.HasPrefix(trimmed, "Completion check FAILED:")
}

func cappedTokens(target, configuredMax int) int {
	if configuredMax <= 0 {
		return target
	}
	if target > configuredMax {
		return configuredMax
	}
	return target
}

func configuredOrDefault(configuredMax int) int {
	if configuredMax > 0 {
		return configuredMax
	}
	return 4096
}

func boolPtr(v bool) *bool { return &v }
