package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultOutputMaxBytes is the default byte cap applied to every tool result
	// at the registry boundary. Chosen as the midpoint of the 4–16 KiB range
	// observed across picobot/codex/openhuman/cli-printing-press research.
	DefaultOutputMaxBytes = 8192
	// DefaultOutputMaxRows is the default row (line) cap applied to every tool
	// result at the registry boundary.
	DefaultOutputMaxRows = 20
)

// OutputCap controls how the registry trims a tool's result before it reaches
// the agent loop. Zero values resolve to the package defaults (8192 bytes,
// 20 rows). Bypass=true skips the cap entirely (for tools that return paths or
// IDs rather than user-visible content, e.g. doc action=xlsx/docx/pdf).
type OutputCap struct {
	MaxBytes int  // 0 → DefaultOutputMaxBytes
	MaxRows  int  // 0 → DefaultOutputMaxRows
	Bypass   bool // true → skip cap entirely
}

// BoundOutput trims content to maxBytes (UTF-8-safe) and then to maxRows
// lines, appending a human- and LLM-readable truncation marker when clipping
// occurred. The bool return is true when the content was trimmed.
//
// Empty content is returned unchanged. Content that already ends with a
// [truncated: ...] marker (upstream truncation) is returned unchanged so we
// never double-mark.
//
// Row counting uses '\n' splits. For v0 this is line-based even for JSON
// output; a future version may add a JSON-item row-counter for structured
// search results.
func BoundOutput(content string, maxBytes int, maxRows int) (string, bool) {
	if content == "" {
		return content, false
	}
	// Don't double-mark content already truncated upstream.
	if hasTruncationMarker(content) {
		return content, false
	}

	totalBytes := len(content)
	totalRows := strings.Count(content, "\n") + 1

	if totalBytes <= maxBytes && totalRows <= maxRows {
		return content, false
	}

	clipped := content

	// Byte cap first (UTF-8-safe — never break mid-rune).
	if len(clipped) > maxBytes {
		clipped = safeTruncateUTF8(clipped, maxBytes)
	}

	// Row cap after byte clip.
	if strings.Count(clipped, "\n")+1 > maxRows {
		parts := strings.SplitN(clipped, "\n", maxRows+1)
		clipped = strings.Join(parts[:maxRows], "\n")
	}

	actualBytes := len(clipped)
	actualRows := strings.Count(clipped, "\n") + 1
	pct := 0
	if totalBytes > 0 {
		pct = actualBytes * 100 / totalBytes
	}

	marker := fmt.Sprintf(
		"\n\n[truncated: %d of %d bytes (%d%%), %d of %d rows."+
			" Re-run with a narrower query OR ask the user what subset they want.]",
		actualBytes, totalBytes, pct, actualRows, totalRows,
	)
	return clipped + marker, true
}

// safeTruncateUTF8 clips s to at most maxBytes without splitting a multi-byte
// rune. Steps backward from maxBytes until it lands on a rune-start byte.
func safeTruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	i := maxBytes
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}

// hasTruncationMarker returns true when the tail of content already contains a
// [truncated: ...] marker so we never stack two markers on the same payload.
func hasTruncationMarker(content string) bool {
	tail := content
	if len(tail) > 300 {
		tail = tail[len(tail)-300:]
	}
	return strings.Contains(tail, "[truncated:")
}

// applyOutputCap resolves the cap from a ToolDefinition and applies
// BoundOutput. This is the single call site in Registry.Execute.
func applyOutputCap(content string, cap OutputCap) (string, bool) {
	if cap.Bypass {
		return content, false
	}
	maxBytes := cap.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultOutputMaxBytes
	}
	maxRows := cap.MaxRows
	if maxRows <= 0 {
		maxRows = DefaultOutputMaxRows
	}
	return BoundOutput(content, maxBytes, maxRows)
}
