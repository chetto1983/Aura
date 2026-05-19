package tokenjuice

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/uniseg"
)

// Package-level compiled ANSI regexes (compiled once, used per call).
var (
	// ansiEscape matches CSI sequences (ESC[ ... final-byte) and single-char Fe sequences.
	ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|[@-_])`)
	// ansiOSC matches OSC sequences terminated by BEL.
	ansiOSC = regexp.MustCompile(`\x1b][^\x07]*\x07`)
)

// countTextChars counts Unicode grapheme clusters in s.
// This is the canonical limit/clamp unit (algorithm spec §4.3) — every
// size-based decision uses grapheme counts to preserve CJK, emoji, ZWJ sequences.
func countTextChars(s string) int {
	return uniseg.GraphemeClusterCount(s)
}

// stripAnsi removes ANSI/VT100 escape sequences from s.
// Pipeline: OSC sequences → CSI + single-char ESC → lone ESC bytes.
func stripAnsi(s string) string {
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiEscape.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "\x1b", "")
}

// normalizeLines replaces CRLF with LF, splits on LF, and trims trailing
// whitespace from each line. Empty interior lines are preserved.
func normalizeLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return lines
}

// trimEmptyEdges drops blank lines (empty or whitespace-only) at the start
// and end of lines. Returns an empty slice if everything is blank.
func trimEmptyEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// dedupeAdjacent collapses consecutive identical lines, keeping the first.
func dedupeAdjacent(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	result := make([]string, 0, len(lines))
	result = append(result, lines[0])
	for i := 1; i < len(lines); i++ {
		if lines[i] != result[len(result)-1] {
			result = append(result, lines[i])
		}
	}
	return result
}

// headTail keeps the first head lines, a "K lines omitted" marker, and the
// last tail lines. If len(lines) ≤ head+tail the slice is returned unchanged.
func headTail(lines []string, head, tail int) []string {
	if len(lines) <= head+tail {
		return lines
	}
	k := len(lines) - head - tail
	result := make([]string, 0, head+1+tail)
	result = append(result, lines[:head]...)
	result = append(result, fmt.Sprintf("... %d lines omitted ...", k))
	result = append(result, lines[len(lines)-tail:]...)
	return result
}

// graphemeByteEnd returns the byte offset just after the first n grapheme
// clusters in s. Returns len(s) if s has fewer than n clusters.
func graphemeByteEnd(s string, n int) int {
	if n <= 0 {
		return 0
	}
	g := uniseg.NewGraphemes(s)
	count := 0
	for g.Next() {
		count++
		if count == n {
			_, end := g.Positions()
			return end
		}
	}
	return len(s)
}

// graphemeByteStart returns the byte offset where the last n grapheme clusters
// of s begin. Returns 0 if s has ≤ n clusters.
func graphemeByteStart(s string, n int) int {
	total := countTextChars(s)
	if total <= n {
		return 0
	}
	return graphemeByteEnd(s, total-n)
}

// clampText performs tail truncation at maxChars grapheme clusters.
// Short inputs (≤ maxChars) are returned unchanged.
// After cutting, the head is backed up to the last newline when it falls
// in the second half of the truncated portion (algorithm spec §4.2).
func clampText(s string, maxChars int) string {
	const suffix = "\n... truncated ..."
	if maxChars <= 0 || countTextChars(s) <= maxChars {
		return s
	}
	suffixChars := countTextChars(suffix)
	bodyMax := maxChars - suffixChars
	if bodyMax <= 0 {
		bodyMax = 1
	}
	cutByte := graphemeByteEnd(s, bodyMax)
	head := s[:cutByte]
	if nl := strings.LastIndex(head, "\n"); nl >= len(head)/2 {
		head = head[:nl]
	}
	return head + suffix
}

// clampTextMiddle truncates at maxChars grapheme clusters, keeping 70% head
// and 30% tail with a central marker. Short inputs are returned unchanged.
func clampTextMiddle(s string, maxChars int) string {
	const marker = "\n... omitted ...\n"
	if maxChars <= 0 || countTextChars(s) <= maxChars {
		return s
	}
	markerChars := countTextChars(marker)
	bodyMax := maxChars - markerChars
	if bodyMax < 2 {
		bodyMax = 2
	}
	headMax := (bodyMax*7 + 9) / 10 // ceil(0.7 * bodyMax)
	tailMax := bodyMax - headMax

	// Head: first headMax graphemes, trim to line boundary in second half
	headEnd := graphemeByteEnd(s, headMax)
	head := s[:headEnd]
	if nl := strings.LastIndex(head, "\n"); nl >= len(head)/2 {
		head = head[:nl]
	}

	// Tail: last tailMax graphemes, trim to line boundary in first half
	tailStart := graphemeByteStart(s, tailMax)
	tail := s[tailStart:]
	if nl := strings.Index(tail, "\n"); nl >= 0 && nl <= len(tail)/2 {
		tail = tail[nl+1:]
	}

	return head + marker + tail
}

// safeClamp is a grapheme-aware tail truncation (alias for clampText).
// Used when the caller wants a safe cut that never splits a cluster.
func safeClamp(s string, maxChars int) string {
	return clampText(s, maxChars)
}

// pluralize returns "N noun" with basic English inflection rules.
// Special-cases: already-pluralized words (passed/failed/skipped) → no suffix.
func pluralize(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	lower := strings.ToLower(noun)
	if strings.HasSuffix(lower, "passed") ||
		strings.HasSuffix(lower, "failed") ||
		strings.HasSuffix(lower, "skipped") {
		return fmt.Sprintf("%d %s", count, noun)
	}
	var plural string
	switch {
	case strings.HasSuffix(lower, "sh") || strings.HasSuffix(lower, "ch") ||
		strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z"):
		plural = noun + "es"
	case len(lower) >= 2 && strings.HasSuffix(lower, "y") && !isVowel(rune(lower[len(lower)-2])):
		plural = noun[:len(noun)-1] + "ies"
	default:
		plural = noun + "s"
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
