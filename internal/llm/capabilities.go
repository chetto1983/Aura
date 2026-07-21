package llm

import (
	"os"
	"strconv"
	"strings"
)

// EstimatorCapability records a chat backend's token-accounting contract: whether
// Aura can trust a provider-side tokenizer, and how many tokens of headroom the L2
// hard cap must reserve for the divergence between Aura's lexical/tiktoken count
// and the backend's real tokenizer. It is the surviving seed of the (removed)
// Phase-42 capability preflight, now load-bearing for the Wave-1.10 token-accounting
// layer (honest per-provider token estimation).
type EstimatorCapability struct {
	// ID/Version identify the capability row (e.g. "llamacpp"/"1").
	ID, Version string
	// ProviderTokenizer is true when the backend reports authoritative token counts
	// Aura can trust (OpenRouter usage). When false, Aura's own estimate is the only
	// signal and DeclaredErrorTokens must pad the budget.
	ProviderTokenizer bool
	// DeclaredErrorTokens is the fixed L2 hard-cap headroom reserved for the tokenizer
	// divergence on this backend. On the local llama.cpp path there is NO provider-side
	// overflow net (OpenRouter's middle-out, Wave 1.11), so an Aura under-count is a
	// HARD local overflow — the reserve keeps the L2.5 lexical trim strictly under the
	// model's real window. Aura counts with tiktoken; a local SentencePiece/BPE model
	// (e.g. Gemma-4) tokenizes the same text differently, diverging most on non-English
	// text and code.
	DeclaredErrorTokens int
}

const (
	// envLlamaCppErrorReserve overrides the llama.cpp hard-cap reserve (env-note: a
	// token-budget knob in the AURA_MODEL_* domain, like AURA_MODEL_CONTEXT_WINDOW).
	// Raise it for a model/tokenizer that under-counts more, or when a long local
	// turn still overflows; 0 disables the reserve.
	envLlamaCppErrorReserve = "AURA_MODEL_LLAMACPP_ERROR_RESERVE_TOKENS"
	// defaultLlamaCppErrorReserve is the fixed reserve when the knob is unset. It sits
	// on top of the SPEC Req#10 max(MaxOutputTokens,20000)+13000 reservations as an
	// extra safety margin for the tiktoken-vs-local-tokenizer gap on the netless local
	// path; the operator tunes it per model via the env knob.
	defaultLlamaCppErrorReserve = 4096
)

// llamaCppEstimator is the token-accounting capability for the local llama.cpp chat
// path: no trustworthy provider tokenizer, and a fixed reserve for the
// tiktoken-vs-local-tokenizer divergence.
func llamaCppEstimator() EstimatorCapability {
	return EstimatorCapability{
		ID:                  "llamacpp",
		Version:             "1",
		ProviderTokenizer:   false,
		DeclaredErrorTokens: llamaCppErrorReserveTokens(),
	}
}

// llamaCppErrorReserveTokens reads the reserve from the env knob, falling back to the
// default. A set-but-malformed or negative value falls back (the reserve is a safety
// margin, not budget-load-bearing — a typo must not block a turn).
func llamaCppErrorReserveTokens() int {
	if v := strings.TrimSpace(os.Getenv(envLlamaCppErrorReserve)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultLlamaCppErrorReserve
}

// ProviderErrorReserveTokens returns the L2 hard-cap headroom to reserve for the
// configured chat backend's token-estimation error. Only the local llama.cpp path
// needs it — OpenRouter has a provider-side overflow net (Wave 1.11 middle-out) and
// reports authoritative usage, so it (and an unrecognized target) reserve nothing.
func ProviderErrorReserveTokens(cfg Config) int {
	if ReasoningTarget(cfg.Provider, cfg.BaseURL) == ReasoningTargetLlamaCpp {
		return llamaCppEstimator().DeclaredErrorTokens
	}
	return 0
}
