package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aura/aura/internal/storage/sweep"
)

const (
	// SpillThresholdBytes is the payload size above which tool output is
	// persisted to disk instead of being inlined (or truncated inline).
	// Below this threshold, limitToolContent / TruncateMiddle handles capping.
	SpillThresholdBytes = 8 * 1024 // 8 KB

	// spillPreviewBytes is the max byte length of the content preview embedded
	// in the spill envelope so the LLM can gauge the result without loading
	// the full file.
	spillPreviewBytes = 1200
)

// SpillOutput writes content to <baseDir>/tool-results/<sessionID>/<callID>.txt
// and returns a reference envelope for the LLM. The LRU+age sweeper runs
// piggybacked on every successful persist (no separate daemon required).
//
// The returned envelope has the form:
//
//	[tool result spilled to disk]
//	path: /workspace/tool-results/<sessionID>/<callID>.txt
//	original_size_bytes: N
//	preview (first 1200 chars):
//	<preview>
//
// On error the caller should fall back to inline truncation.
func SpillOutput(baseDir, sessionID, callID, content string) (string, error) {
	// Guard against path traversal in caller-supplied IDs.
	if strings.Contains(sessionID, "..") || strings.Contains(callID, "..") {
		return "", fmt.Errorf("spill: invalid session or call ID (path traversal)")
	}

	dir := filepath.Join(baseDir, "tool-results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("spill: mkdir %s: %w", dir, err)
	}

	dest := filepath.Join(dir, callID+".txt")
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("spill: write %s: %w", dest, err)
	}

	// Sweep piggybacked on persist — evicts sessions beyond LRU cap or TTL.
	_ = sweep.SweepSessions(filepath.Join(baseDir, "tool-results"))

	preview := content
	if len(preview) > spillPreviewBytes {
		preview = preview[:spillPreviewBytes]
	}

	llmPath := "/workspace/tool-results/" + sessionID + "/" + callID + ".txt"
	return fmt.Sprintf(
		"[tool result spilled to disk]\npath: %s\noriginal_size_bytes: %d\npreview (first 1200 chars):\n%s",
		llmPath, len(content), preview,
	), nil
}
