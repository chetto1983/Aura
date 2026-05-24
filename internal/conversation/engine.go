// engine.go defines the ContextEngine ABC and its default implementation.
// The agent loop calls UpdateFromResponse after each LLM response, then
// ShouldCompress before the next LLM call; when true, Compress is called
// and its result replaces the message slice fed to the model.
//
// Secondary interfaces (ContextEnginePreflight, ContextEngineLifecycle) are
// optional — callers type-assert rather than requiring every implementation
// to provide every method.
package conversation

import "github.com/aura/aura/internal/llm"

// ContextEngine is the compression-strategy ABC.
// Each implementation decides when and how to shrink the conversation history.
type ContextEngine interface {
	// Name identifies the engine in logs and /api/metrics.
	Name() string
	// UpdateFromResponse is called after every LLM response with the token
	// usage reported by the provider. Engines that track cumulative token
	// budget update their counters here.
	UpdateFromResponse(usage llm.TokenUsage)
	// ShouldCompress returns true when the engine wants to compress the
	// conversation history before the next LLM call. The promptTokens
	// argument carries the value the loop chooses to pass — DefaultContextEngine
	// treats it as a message count (len(messages)); token-aware engines
	// (AutoCompactEngine) treat it as an actual token count.
	ShouldCompress(promptTokens int) bool
	// Compress reduces the message slice and returns the compressed version.
	// currentTokens is the same value passed to ShouldCompress.
	// focusTopic is the latest user message text (passed to summary prompts
	// by LLM-backed engines; DefaultContextEngine ignores it).
	// Implementations MUST NOT mutate the input slice.
	Compress(messages []llm.Message, currentTokens int, focusTopic string) []llm.Message
}

// ContextEnginePreflight is an optional secondary interface.
// Engines that want an early eligibility check before the loop calls
// ShouldCompress implement this; type-assert at the call site.
type ContextEnginePreflight interface {
	ShouldCompressPreflight(messages []llm.Message) bool
}

// ContextEngineLifecycle is an optional secondary interface.
// Engines with session-scoped state implement this for lifecycle callbacks.
type ContextEngineLifecycle interface {
	OnSessionStart()
	OnSessionEnd()
	OnSessionReset()
}

// MaxConversationMessages is the default message-count cap used by
// DefaultContextEngine. Shared with the conversation.Config.MaxMessages path
// so both paths agree on the default cap without importing each other.
const MaxConversationMessages = 50

// DefaultContextEngine wraps the current picobot-style 50-message sliding-
// window behavior. ShouldCompress interprets its argument as a message count
// (the loop passes len(messages)); the Cap field overrides the default 50.
//
// This is the foundation engine: it produces the same trimmed slice that
// conversation.Context.enforceMessageCap would produce, so wiring it into
// the agent loop preserves byte-identical message sequences while making the
// compression strategy swappable for US-CTX-02..05.
type DefaultContextEngine struct {
	// Cap is the maximum number of messages (including the system message)
	// to keep after compression. Zero uses MaxConversationMessages (50).
	Cap int

	// Public counters updated by UpdateFromResponse — read by metrics and tests.
	LastPromptTokens     int
	LastCompletionTokens int
	LastTotalTokens      int
	CompressionCount     int
}

// NewDefaultContextEngine creates a DefaultContextEngine with the given cap.
// cap ≤ 0 defaults to MaxConversationMessages.
func NewDefaultContextEngine(cap int) *DefaultContextEngine {
	return &DefaultContextEngine{Cap: cap}
}

func (e *DefaultContextEngine) effectiveCap() int {
	if e.Cap > 0 {
		return e.Cap
	}
	return MaxConversationMessages
}

// Name satisfies ContextEngine.
func (e *DefaultContextEngine) Name() string { return "default" }

// UpdateFromResponse records the last LLM round's token usage.
func (e *DefaultContextEngine) UpdateFromResponse(usage llm.TokenUsage) {
	e.LastPromptTokens = usage.PromptTokens
	e.LastCompletionTokens = usage.CompletionTokens
	e.LastTotalTokens = usage.TotalTokens
}

// ShouldCompress returns true when the message count exceeds Cap.
// The caller (loop.go) passes len(messages) as promptTokens.
func (e *DefaultContextEngine) ShouldCompress(promptTokens int) bool {
	return promptTokens > e.effectiveCap()
}

// Compress slides off the oldest non-system messages until len ≤ Cap.
// Preserves tool-call / tool-result pairs by moving the split point via
// toolSafeBoundary — the same invariant as conversation.Context.enforceMessageCap.
// Returns the input slice unchanged when no compression is needed.
func (e *DefaultContextEngine) Compress(messages []llm.Message, _ int, _ string) []llm.Message {
	cap := e.effectiveCap()
	if len(messages) <= cap {
		return messages
	}

	// Isolate the system message (if present) — it is never counted against cap.
	hasSystem := len(messages) > 0 && messages[0].Role == "system"
	body := messages
	if hasSystem {
		body = body[1:]
	}

	if len(body) <= cap {
		return messages
	}

	// Keep the LAST cap messages; advance split past any stranded tool results.
	split := len(body) - cap
	split = toolSafeBoundary(body, split)
	if split <= 0 {
		return messages
	}

	kept := body[split:]
	out := make([]llm.Message, 0, 1+len(kept))
	if hasSystem {
		out = append(out, messages[0])
	}
	out = append(out, kept...)
	e.CompressionCount++
	return out
}
