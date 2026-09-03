package prompt

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
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

// ApplyAdaptiveReasoning applies a precomputed tier to the main request. It sets ONLY
// the reasoning EFFORT (off/low/high) — it NEVER touches max_tokens: capping the output
// budget by tier truncated tool-call arguments mid-JSON (the 203-turn disaster,
// 2026-06-14), so the operator-configured cfg.MaxTokens is left untouched. It never
// forces ToolChoice="none" either: the hot manifest keeps active tools and deferred tool
// names visible, so the model can still call tool_search when needed.
//
// It gates on the GENERALIZED IsReasoningTarget, like ApplyFixedReasoning already did.
// It used to gate on IsOpenRouterReasoningTarget, on the reasoning that "local backends
// never ran the classifier tiering" — but the classifier runs on the CONVERSATION, not on
// the backend, and the per-provider translation of a ReasoningConfig has existed on every
// arm for as long as the restriction did: OpenRouter takes reasoning{effort},
// Ollama takes reasoning_effort, and llamaCppReasoning maps every effort symbol onto
// thinking_budget_tokens / enable_thinking. The oracle was choosing a tier and the gate
// was throwing it away.
//
// What that cost, measured live on Ollama 0.33.2 with gemma4:31b-cloud on 2026-08-31:
// req.Reasoning stayed empty, appendReasoningOptions sent no reasoning_effort, and the
// model's default is not to think — so adaptive reasoning was silently OFF on every turn,
// for the life of the deployment. The same call WITH reasoning_effort comes back carrying
// a `reasoning` field, so the capability was there and unused. The absurdity was that a
// manual selection from the composer DID reach Ollama (ApplyFixedReasoning), so the
// automatic path was the only one restricted.
//
// What a model can actually honour is a separate question, and this function is now the
// place it gets answered on the request path. It used to say the tier was "still bounded"
// by llm.ReasoningCapabilitySource -- it was not. That source fed the cockpit only (the
// effort dropdown, and validation of an EXPLICIT user choice); nothing between the
// classifier and the wire ever consulted it. So a greeting on a reasoning-mandatory model
// was classified "none", sent as effort "none", and refused with HTTP 400 by the provider
// -- measured 2026-09-03 on z-ai/glm-5.3-flash and google/gemini-3.8-flash. cfg carries
// the model's published set (resolved at every model change) and ClampReasoningEffort
// substitutes the nearest accepted effort; a model that published nothing is left alone.
func ApplyAdaptiveReasoning(req *llm.Request, provider string, cfg llm.Config, tier ReasoningTier) {
	if !cfg.AdaptiveReasoning {
		return
	}
	if !IsReasoningTarget(provider, cfg.BaseURL) || !tier.Valid() {
		// SAY SO. The adaptive path logged nothing at all, so a backend it silently
		// skipped was indistinguishable from a model that had chosen not to think — which
		// is exactly how it went unnoticed on Ollama that adaptive reasoning had never
		// once been applied.
		slog.Debug("adaptive reasoning: not applied",
			"target", llm.ReasoningTarget(provider, cfg.BaseURL), "tier", string(tier))
		return
	}
	req.Reasoning = tier.reasoning(cfg.ShowReasoning)
	wanted := req.Reasoning.Effort
	req.Reasoning.Effort = cfg.ClampReasoningEffort(wanted)
	slog.Info("adaptive reasoning: tier applied",
		"target", llm.ReasoningTarget(provider, cfg.BaseURL),
		"tier", string(tier), "effort", string(req.Reasoning.Effort),
		// Name the substitution when one happened: an effort that silently differs from
		// the tier is the kind of thing that has to be readable in a log, not inferred.
		"requested", string(wanted), "clamped", wanted != req.Reasoning.Effort)
}

// ApplyFixedReasoning forces a per-turn reasoning EFFORT chosen by the user (the web
// Composer selector, D-02) onto the main request, BYPASSING the adaptive classifier.
// Unlike ApplyAdaptiveReasoning it is orthogonal to cfg.AdaptiveReasoning — an explicit
// selection must fire even when adaptive tiering is off — and it gates on the GENERALIZED
// IsReasoningTarget, so a fixed effort reaches every supported reasoning backend
// (D-08). An empty effort is the "auto" sentinel: a no-op that leaves the adaptive/plain
// path byte-identical (D-04, zero regression). exclude is derived from cfg.ShowReasoning
// EXACTLY as ReasoningTier.reasoning() does — the selector controls effort, never CoT
// visibility (D-10). Like the adaptive path it never touches MaxTokens (the 2026-06-14
// contract: capping the output budget by tier truncated tool-call arguments mid-JSON).
func ApplyFixedReasoning(req *llm.Request, provider string, cfg llm.Config, effort llm.ReasoningEffort) {
	if effort == "" || !IsReasoningTarget(provider, cfg.BaseURL) {
		return
	}
	// An explicit choice is clamped too. The cockpit only offers efforts the model
	// advertises, but a stale tab or a direct API call can still name one it does not,
	// and the honest answer to that is the nearest accepted gear rather than a 400.
	req.Reasoning = llm.ReasoningConfig{
		Effort: cfg.ClampReasoningEffort(effort), Exclude: new(!cfg.ShowReasoning),
	}
}

// IsOpenRouterReasoningTarget reports whether provider/baseURL is the OpenRouter
// reasoning projection. It delegates to the neutral llm.ReasoningTarget classifier
// (landed by 37E-02) so OpenRouter recognition has a single source of truth; the
// result is byte-identical to the historical inline string check, so the ADAPTIVE-path
// callers (ApplyAdaptiveReasoning, adaptiveReasoningTier) are unchanged (D-04).
func IsOpenRouterReasoningTarget(provider, baseURL string) bool {
	return llm.ReasoningTarget(provider, baseURL) == llm.ReasoningTargetOpenRouter
}

// IsReasoningTarget reports whether provider/baseURL is ANY recognized reasoning
// backend (D-08). BOTH the fixed per-turn effort override and the adaptive path gate on
// it: the adaptive path was OpenRouter-only until 2026-08-31, which left the classifier
// choosing a tier that no other backend ever received (see ApplyAdaptiveReasoning).
func IsReasoningTarget(provider, baseURL string) bool {
	switch llm.ReasoningTarget(provider, baseURL) {
	case llm.ReasoningTargetOpenRouter, llm.ReasoningTargetLlamaCpp, llm.ReasoningTargetOllama:
		return true
	default:
		return false
	}
}

// reasoning maps a tier to the OpenRouter reasoning object. Verified live against
// DeepSeek-V4 Flash on 2026-06-11 (scripts/deepseek_reasoning_probe.py + controls;
// regression-guarded by adaptive_reasoning_live_e2e_test.go):
//
//   - effort:"none" is the ONLY working off-switch on this path — DeepSeek's native
//     thinking:{type:disabled} toggle is dropped by OpenRouter. None tier => 0 tokens.
//   - DeepSeek collapses effort low/medium -> high server-side, so the Low tier's
//     effort label does NOT request a lighter gear; the model self-scales reasoning to
//     the turn (59 tokens on a greeting -> 8260 on a hard puzzle), so on DeepSeek the
//     Low/High labels are effectively the same gear today. Keep them anyway: they are
//     provider-neutral and forward-compatible with models that DON'T collapse.
//   - exclude:true redacts the chain-of-thought from the wire but does NOT cap it; and
//     max_tokens bounds only the VISIBLE answer (probe: reasoning ran to ~8351 tokens
//     past a 4096 cap and still answered cleanly). Because that ceiling truncates the
//     visible output — including tool-call arguments — the tier NEVER sets it; the
//     output budget stays at the operator's cfg.MaxTokens (the 2026-06-14 fix).
//
// exclude is the inverse of showReasoning (cfg.ShowReasoning / AURA_SHOW_REASONING):
// exclude:true (default) withholds the reasoning text — verified live to yield ZERO
// reasoning deltas in the stream, so the consumer surfaces (CLI 💭, Telegram live
// window) see nothing; exclude:false streams the real CoT for display. The reasoning
// tokens are generated and billed either way (exclude only gates the text), so
// surfacing reasoning costs only the bandwidth of the deltas, not extra tokens.
func (t ReasoningTier) reasoning(showReasoning bool) llm.ReasoningConfig {
	exclude := new(!showReasoning)
	switch t {
	case ReasoningTierHigh:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh, Exclude: exclude}
	case ReasoningTierLow:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortLow, Exclude: exclude}
	default:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortNone, Exclude: exclude}
	}
}

// LastGenuineUserContent returns the newest real user request, skipping synthetic
// budget, workspace, time, recovery, and completion-check nudges.
func LastGenuineUserContent(history []llm.Message) string {
	for _, v := range slices.Backward(history) {
		m := v
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
