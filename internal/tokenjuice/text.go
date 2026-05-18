package tokenjuice

import "github.com/rivo/uniseg"

// countTextChars counts Unicode grapheme clusters in s.
// This is the canonical limit/clamp unit per algorithm spec §4.3 —
// every size-based decision (maxInlineChars, clamp boundaries) uses grapheme
// counts rather than bytes or rune counts to preserve CJK, emoji, and ZWJ
// sequences. Full ANSI-strip + line helpers are implemented in US-TJ03.
func countTextChars(s string) int {
	return uniseg.GraphemeClusterCount(s)
}
