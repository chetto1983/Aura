// auto_compact.go implements AutoCompactEngine: a token-budget ContextEngine
// (WHEN engine) that triggers compaction at 50% of the model context window.
// It wraps a *ContextCompressor for the HOW; AutoCompactEngine decides WHEN.
//
// Threshold semantics: modelContextWindow * AURA_CTX_COMPACT_PERCENT (USER LOCKED
// at 50%, 2026-05-24). Codex references: core/src/session/turn.rs:301-358 + :655-705.
package conversation

import (
	"log/slog"
	"sync"

	"github.com/aura/aura/internal/llm"
)

const (
	// ScopeTotal tracks all accumulated tokens (overlays + conversation body)
	// against the compaction threshold. Default scope.
	ScopeTotal = "total"
	// ScopeBodyAfterPrefix excludes the first-call prompt baseline from the
	// effective count, mirroring Codex prefill_input_tokens semantics. Useful
	// when overlays are large but stable and should not count toward the body
	// budget that drives compaction.
	ScopeBodyAfterPrefix = "body_after_prefix"
)

// AutoCompactEngine is a token-budget ContextEngine that triggers compaction
// when accumulated tokens reach ThresholdTokens. It wraps a ContextCompressor
// for the actual summarization; AutoCompactEngine provides the WHEN gate.
//
// Two-scope budget:
//   - ScopeTotal:           effective = sum(TotalTokens per call)
//   - ScopeBodyAfterPrefix: effective = sum(TotalTokens) - first_call_PromptTokens
//
// The last user message is always preserved across compaction (Codex
// BeforeLastUserMessage invariant) — the model always sees the current request.
type AutoCompactEngine struct {
	inner           *ContextCompressor
	ThresholdTokens int    // modelContextWindow * compactPercent
	Scope           string // ScopeTotal | ScopeBodyAfterPrefix

	mu              sync.Mutex
	cumulativeTotal int  // sum of TotalTokens from all UpdateFromResponse calls
	prefixBaseline  int  // first-call PromptTokens; baseline for ScopeBodyAfterPrefix
	prefixSet       bool // true once prefixBaseline is captured

	// Public counters — read by metrics and tests.
	LastPromptTokens     int
	LastCompletionTokens int
	LastTotalTokens      int
	CompressionCount     int
}

// NewAutoCompactEngine creates an AutoCompactEngine wrapping the given
// ContextCompressor. scope defaults to ScopeTotal when empty or unrecognized.
// thresholdTokens ≤ 0 disables compaction (ShouldCompress always returns false).
func NewAutoCompactEngine(inner *ContextCompressor, thresholdTokens int, scope string) *AutoCompactEngine {
	if scope != ScopeBodyAfterPrefix {
		scope = ScopeTotal
	}
	return &AutoCompactEngine{
		inner:           inner,
		ThresholdTokens: thresholdTokens,
		Scope:           scope,
	}
}

// Name satisfies ContextEngine.
func (e *AutoCompactEngine) Name() string { return "auto_compact" }

// UpdateFromResponse records LLM token usage and accumulates the running total.
// On the first call with non-zero PromptTokens, captures PromptTokens as the
// prefix baseline for ScopeBodyAfterPrefix tracking (Codex prefill_input_tokens
// pattern: the static system-prompt + overlay tokens that should be "free").
func (e *AutoCompactEngine) UpdateFromResponse(usage llm.TokenUsage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.LastPromptTokens = usage.PromptTokens
	e.LastCompletionTokens = usage.CompletionTokens
	e.LastTotalTokens = usage.TotalTokens
	e.cumulativeTotal += usage.TotalTokens
	if !e.prefixSet && usage.PromptTokens > 0 {
		e.prefixBaseline = usage.PromptTokens
		e.prefixSet = true
	}
	if e.inner != nil {
		e.inner.UpdateFromResponse(usage)
	}
}

// ShouldCompress returns true when the accumulated token budget exceeds
// ThresholdTokens. The promptTokens argument is ignored; AutoCompactEngine
// uses its own cumulative state from UpdateFromResponse calls (unlike
// DefaultContextEngine which uses the passed message count).
func (e *AutoCompactEngine) ShouldCompress(_ int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ThresholdTokens <= 0 || e.cumulativeTotal <= 0 {
		return false
	}
	effective := e.cumulativeTotal
	if e.Scope == ScopeBodyAfterPrefix && e.prefixSet {
		effective = e.cumulativeTotal - e.prefixBaseline
		if effective <= 0 {
			return false
		}
	}
	return effective > e.ThresholdTokens
}

// Compress delegates to the inner ContextCompressor and ensures the last user
// message survives compaction (Codex BeforeLastUserMessage invariant): even when
// the compressor replaces the full conversation body with a summary, the model
// always receives the current user request as its final input message.
func (e *AutoCompactEngine) Compress(messages []llm.Message, currentTokens int, focusTopic string) []llm.Message {
	if e.inner == nil || len(messages) < 2 {
		return messages
	}

	// Snapshot the last user message before the compressor may drop it.
	lastUser, hasLastUser := lastUserMessage(messages)

	// Delegate HOW to the inner compressor.
	originalData := &messages[0]
	compressed := e.inner.Compress(messages, currentTokens, focusTopic)

	// Detect whether the compressor actually changed the slice (success path
	// returns a newly allocated slice; failure path returns the input unchanged).
	compressionHappened := len(compressed) > 0 && &compressed[0] != originalData

	if compressionHappened {
		// Re-append last user message if the compressor dropped it.
		if hasLastUser && !containsUserContent(compressed, lastUser.Content) {
			out := make([]llm.Message, len(compressed)+1)
			copy(out, compressed)
			out[len(compressed)] = lastUser
			compressed = out
		}
		e.mu.Lock()
		e.CompressionCount++
		e.mu.Unlock()
		slog.Debug("auto_compact: conversation compressed",
			"msgs_before", len(messages),
			"msgs_after", len(compressed),
			"threshold_tokens", e.ThresholdTokens,
			"cumulative_tokens", e.cumulativeTotal,
		)
	}
	return compressed
}

// --- package-private helpers -------------------------------------------------

// lastUserMessage returns the last user-role message in msgs and true,
// or a zero value and false when no user message is found.
func lastUserMessage(msgs []llm.Message) (llm.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i], true
		}
	}
	return llm.Message{}, false
}

// containsUserContent reports whether any user-role message in msgs has
// content equal to target. Used to detect whether the last user message
// survived the compressor's summarization pass.
func containsUserContent(msgs []llm.Message, target string) bool {
	for _, m := range msgs {
		if m.Role == "user" && m.Content == target {
			return true
		}
	}
	return false
}
