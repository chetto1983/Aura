package conversations

import (
	"strings"
	"unicode/utf8"
)

// Excerpt windows content around the first case-insensitive occurrence of query (±60 bytes),
// falling back to the first 120 when the literal substring is absent, as on a fuzzy match
// (D-A5-03: pg_trgm has no ts_headline). `aura chat search` and Telegram's /search render
// search hits through it, so the two surfaces show the same excerpt for the same hit: until
// 2026-10-26 each carried its own copy and only a test kept them equal.
func Excerpt(content, query string) string {
	const window = 60
	flat := strings.Join(strings.Fields(content), " ")
	idx := strings.Index(strings.ToLower(flat), strings.ToLower(query))
	if idx < 0 {
		return clampExcerpt(flat, 0, window*2)
	}
	start := max(idx-window, 0)
	end := idx + len(query) + window
	return clampExcerpt(flat, start, end)
}

// clampExcerpt slices [start,end) of s with rune-safe bounds and an ellipsis on each cut side.
func clampExcerpt(s string, start, end int) string {
	if end > len(s) {
		end = len(s)
	}
	if start < 0 {
		start = 0
	}
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	for end > start && end < len(s) && !utf8.RuneStart(s[end]) {
		end--
	}
	out := s[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}
