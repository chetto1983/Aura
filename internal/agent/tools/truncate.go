package truncate

import (
	"fmt"
	"unicode/utf8"
)

// markerOverhead is the byte budget reserved for the ellipsis marker that is
// inserted between head and tail. Sized generously so it holds any realistic
// token-count string (up to 12-digit counts = 39-byte marker; 80 gives ample
// headroom while keeping the math simple).
const markerOverhead = 80

// TruncateMiddle clips s to at most maxBytes by retaining the head and tail and
// inserting an LLM-readable marker ("…N tokens truncated…") in the middle.
// Returns s unchanged when len(s) <= maxBytes or maxBytes <= 0.
// UTF-8-safe: rune boundaries are never split.
func TruncateMiddle(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	if maxBytes <= markerOverhead*2 {
		// Budget too tight for head+marker+tail; fall back to head-only.
		return safeTruncateHead(s, maxBytes)
	}

	half := (maxBytes - markerOverhead) / 2
	head := safeTruncateHead(s, half)
	tail := safeTruncateTail(s, half)

	removed := len(s) - len(head) - len(tail)
	tokens := max(removed/4, 1)
	// The ellipsis characters are U+2026 (…), a single-codepoint 3-byte UTF-8
	// sequence. The model recognises "tokens truncated" as a meta-marker, not
	// content — distinct from both prose and tool output.
	marker := fmt.Sprintf("\n\n…%d tokens truncated…\n\n", tokens)
	return head + marker + tail
}

// TruncateMiddleByTokens is the token-budget variant. It estimates bytes from
// tokens (1 token ≈ 4 bytes, the standard GPT approximation) and delegates to
// TruncateMiddle. Use when callers reason in token units rather than bytes.
func TruncateMiddleByTokens(s string, maxTokens int) string {
	if maxTokens <= 0 {
		return s
	}
	return TruncateMiddle(s, maxTokens*4)
}

// safeTruncateHead clips s to at most maxBytes without splitting a multi-byte
// rune. Steps backward from maxBytes until a rune-start boundary.
func safeTruncateHead(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	i := maxBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}

// safeTruncateTail returns the last (up to) maxBytes of s, advancing past any
// continuation bytes to land on a valid rune-start boundary.
func safeTruncateTail(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
