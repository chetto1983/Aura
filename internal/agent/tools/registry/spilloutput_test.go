package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpillOutputWritesFile verifies that a 100 KB payload is written to disk
// under tool-results/<sessionID>/<callID>.txt and the returned envelope
// contains the correct path, original_size_bytes, and a 1200-char preview.
func TestSpillOutputWritesFile(t *testing.T) {
	base := t.TempDir()
	sessionID := "sess-abc"
	callID := "call-001"

	// 100 KB of repeating ASCII content.
	content := strings.Repeat("a", 100*1024)

	envelope, err := SpillOutput(base, sessionID, callID, content)
	if err != nil {
		t.Fatalf("SpillOutput: %v", err)
	}

	// File must exist at the expected path.
	wantFile := filepath.Join(base, "tool-results", sessionID, callID+".txt")
	data, readErr := os.ReadFile(wantFile)
	if readErr != nil {
		t.Fatalf("spilled file not found at %s: %v", wantFile, readErr)
	}
	if string(data) != content {
		t.Fatalf("file content mismatch: got %d bytes, want %d", len(data), len(content))
	}

	// Envelope must contain the LLM-visible path.
	wantPath := "/workspace/tool-results/" + sessionID + "/" + callID + ".txt"
	if !strings.Contains(envelope, wantPath) {
		t.Errorf("envelope missing path %q\ngot:\n%s", wantPath, envelope)
	}

	// Envelope must mention the original size.
	wantSize := "original_size_bytes: 102400"
	if !strings.Contains(envelope, wantSize) {
		t.Errorf("envelope missing %q\ngot:\n%s", wantSize, envelope)
	}

	// Envelope must contain at most 1200 bytes of preview.
	_, preview, found := strings.Cut(envelope, "preview (first 1200 chars):\n")
	if !found {
		t.Fatalf("envelope missing preview section\ngot:\n%s", envelope)
	}
	if len(preview) > spillPreviewBytes {
		t.Errorf("preview exceeds %d bytes: got %d", spillPreviewBytes, len(preview))
	}
}

// TestSpillOutputSmallPayloadPreview verifies that content shorter than the
// preview cap is included in full in the envelope.
func TestSpillOutputSmallPayloadPreview(t *testing.T) {
	base := t.TempDir()
	content := "short result"

	envelope, err := SpillOutput(base, "s1", "c1", content)
	if err != nil {
		t.Fatalf("SpillOutput: %v", err)
	}
	if !strings.Contains(envelope, content) {
		t.Errorf("envelope should contain full short content\ngot:\n%s", envelope)
	}
}

// TestSpillOutputEnvelopeMarker confirms the envelope leads with the
// LLM-recognisable marker string.
func TestSpillOutputEnvelopeMarker(t *testing.T) {
	base := t.TempDir()
	envelope, err := SpillOutput(base, "s2", "c2", "data")
	if err != nil {
		t.Fatalf("SpillOutput: %v", err)
	}
	if !strings.HasPrefix(envelope, "[tool result spilled to disk]") {
		n := min(60, len(envelope))
		t.Errorf("envelope must start with marker; got: %q", envelope[:n])
	}
}

// TestSpillOutputPathTraversalRejected ensures that session or call IDs
// containing ".." are rejected before any filesystem operation.
func TestSpillOutputPathTraversalRejected(t *testing.T) {
	base := t.TempDir()
	if _, err := SpillOutput(base, "../evil", "c1", "data"); err == nil {
		t.Error("expected error for sessionID with path traversal, got nil")
	}
	if _, err := SpillOutput(base, "s1", "../evil", "data"); err == nil {
		t.Error("expected error for callID with path traversal, got nil")
	}
}
