// Package utf8cut truncates a string to a byte budget without splitting a rune.
//
// Four copies of this had grown in internal/agent, internal/agent/tools (twice) and
// internal/agent/mcptools by 2026-10-26, three of them identical in behaviour. The fourth
// looped utf8.ValidString one byte at a time: on text with an invalid byte before the cut,
// it trimmed back past that byte, which cost O(n²) and threw away valid text after it.
// ValidPrefix is its replacement.
package utf8cut

import (
	"strings"
	"unicode/utf8"
)

// Prefix returns the longest prefix of s that fits in max bytes and ends on a rune start.
// A max of zero or less yields "".
func Prefix(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Ellipsize is Prefix plus "…" when anything was cut, for a value quoted in a message.
func Ellipsize(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return Prefix(s, max) + "…"
}

// ValidPrefix is Prefix over s with each invalid UTF-8 sequence replaced by U+FFFD first, so
// the result is always valid UTF-8. It is for text Aura does not control, such as an MCP
// server's descriptions and errors.
func ValidPrefix(s string, max int) string {
	return Prefix(strings.ToValidUTF8(s, "�"), max)
}
