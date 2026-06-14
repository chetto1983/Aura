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

	req.Reasoning = tier.reasoning(cfg.ShowReasoning)
	req.MaxTokens = tier.maxTokens(cfg.MaxTokens, len(req.Tools) > 0)
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

// reasoning maps a tier to the OpenRouter reasoning object. Verified live against
// DeepSeek-V4 Flash on 2026-06-11 (scripts/deepseek_reasoning_probe.py + controls;
// regression-guarded by adaptive_reasoning_live_e2e_test.go):
//
//   - effort:"none" is the ONLY working off-switch on this path — DeepSeek's native
//     thinking:{type:disabled} toggle is dropped by OpenRouter. None tier => 0 tokens.
//   - DeepSeek collapses effort low/medium -> high server-side, so the Low tier's
//     effort label does NOT request a lighter gear; the model self-scales reasoning to
//     the turn (59 tokens on a greeting -> 8260 on a hard puzzle) and the Low/High
//     tiers differ only by the trailing max_tokens ceiling. Keep the low/high labels:
//     they are provider-neutral and forward-compatible with models that DON'T collapse.
//   - exclude:true redacts the chain-of-thought from the wire but does NOT cap it, and
//     max_tokens bounds only the visible answer (probe: reasoning ran to ~8351 tokens
//     past a 4096 cap and still answered cleanly) — so a deep High turn is never
//     truncated mid-thought at the configured max.
//
// exclude is the inverse of showReasoning (cfg.ShowReasoning / AURA_SHOW_REASONING):
// exclude:true (default) withholds the reasoning text — verified live to yield ZERO
// reasoning deltas in the stream, so the consumer surfaces (CLI 💭, Telegram live
// window) see nothing; exclude:false streams the real CoT for display. The reasoning
// tokens are generated and billed either way (exclude only gates the text), so
// surfacing reasoning costs only the bandwidth of the deltas, not extra tokens.
func (t ReasoningTier) reasoning(showReasoning bool) llm.ReasoningConfig {
	exclude := boolPtr(!showReasoning)
	switch t {
	case ReasoningTierHigh:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh, Exclude: exclude}
	case ReasoningTierLow:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortLow, Exclude: exclude}
	default:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortNone, Exclude: exclude}
	}
}

func (t ReasoningTier) maxTokens(configuredMax int, hasTools bool) int {
	// Tool-capable turns keep the full configured output budget at every tier. The
	// tier ceiling (512/2048) bounds the VISIBLE answer, but a tool call's arguments
	// — a fs_write file body or a shell_exec script — ARE the output and get cut
	// mid-JSON at a low ceiling, yielding "unexpected end of JSON input" and a retry
	// loop the dedup ring can't catch (the 203-turn chart disaster, 2026-06-14). The
	// reasoning EFFORT still tiers via reasoning(); only the output budget is floored.
	if hasTools {
		return configuredOrDefault(configuredMax)
	}
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
		strings.HasPrefix(trimmed, "Your last response was empty.") ||
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
