package llm

import "strings"

// ReasoningTargetKind classifies an LLM backend by how it accepts per-request
// reasoning-effort controls. It is the provider-neutral recognition both the agent
// policy layer (internal/agent/prompt) and the wire layer (internal/llm/openai_compat)
// branch on, kept here in internal/llm — the package both already import — so neither
// has to import the other (a layering smell).
type ReasoningTargetKind int

const (
	// ReasoningTargetNone marks a backend with no recognized reasoning-effort
	// projection (e.g. a local vLLM/DGX endpoint); callers emit no effort knob.
	ReasoningTargetNone ReasoningTargetKind = iota
	// ReasoningTargetOpenRouter marks the OpenRouter cloud path, which accepts the
	// nested reasoning:{effort,...} object (spike 096).
	ReasoningTargetOpenRouter
	// ReasoningTargetLlamaCpp marks a local llama-server chat backend, which accepts
	// chat_template_kwargs:{enable_thinking} + thinking_budget_tokens (spike 095) and
	// IGNORES the OpenRouter reasoning object.
	ReasoningTargetLlamaCpp
	// ReasoningTargetOllama marks Ollama's OpenAI-compatible bridge, which accepts
	// reasoning_effort without OpenRouter or llama.cpp extensions.
	ReasoningTargetOllama
)

// String names the target for logs. Without it an operator reading
// "adaptive reasoning: tier applied target=3" has to go count iota constants to learn
// which backend answered — the value appears in exactly the lines someone reads when
// they are trying to find out whether reasoning reached their model at all.
func (k ReasoningTargetKind) String() string {
	switch k {
	case ReasoningTargetOpenRouter:
		return "openrouter"
	case ReasoningTargetLlamaCpp:
		return "llamacpp"
	case ReasoningTargetOllama:
		return "ollama"
	default:
		return "none"
	}
}

// ReasoningTarget classifies provider/baseURL into a ReasoningTargetKind.
//
// OpenRouter is recognized exactly as the historical prompt.IsOpenRouterReasoningTarget
// gate did — provider=="openrouter" AND a baseURL that is empty or names openrouter.ai
// — so promoting the logic here is behavior-preserving. llama.cpp is keyed on an
// EXPLICIT provider=="llamacpp" (OQ-1), NEVER a baseURL heuristic: the local vLLM/DGX
// path also emits reasoning and shares the non-openrouter.ai shape, so a URL guess would
// misfire and drop the reasoning object on a non-llama backend. Both matches are
// case-insensitive; anything unrecognized is None.
func ReasoningTarget(provider, baseURL string) ReasoningTargetKind {
	if strings.EqualFold(provider, "llamacpp") {
		return ReasoningTargetLlamaCpp
	}
	if strings.EqualFold(provider, "ollama") {
		return ReasoningTargetOllama
	}
	if strings.EqualFold(provider, "openrouter") {
		base := strings.ToLower(strings.TrimSpace(baseURL))
		if base == "" || strings.Contains(base, "openrouter.ai") {
			return ReasoningTargetOpenRouter
		}
	}
	return ReasoningTargetNone
}
