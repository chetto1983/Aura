package conversation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPromptOverlayEmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := LoadPromptOverlay(dir)
	if got != "" {
		t.Errorf("expected empty overlay for empty dir, got %q", got)
	}
}

func TestLoadPromptOverlayMissingDir(t *testing.T) {
	got := LoadPromptOverlay(filepath.Join(t.TempDir(), "nope"))
	if got != "" {
		t.Errorf("expected empty overlay for missing dir, got %q", got)
	}
}

func TestLoadPromptOverlayBlankPathReturnsEmpty(t *testing.T) {
	if got := LoadPromptOverlay(""); got != "" {
		t.Errorf("blank path should return empty, got %q", got)
	}
	if got := LoadPromptOverlay("   "); got != "" {
		t.Errorf("whitespace path should return empty, got %q", got)
	}
}

func TestLoadPromptOverlayReadsKnownFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "SOUL.md"), "I am Aura.")
	mustWrite(t, filepath.Join(dir, "USER.md"), "Operator is Davide.")
	// Unknown file — should be ignored.
	mustWrite(t, filepath.Join(dir, "RANDOM.md"), "ignore me")

	got := LoadPromptOverlay(dir)
	if !strings.Contains(got, "## SOUL") {
		t.Error("missing SOUL section")
	}
	if !strings.Contains(got, "I am Aura.") {
		t.Error("missing SOUL body")
	}
	if !strings.Contains(got, "## USER") {
		t.Error("missing USER section")
	}
	if !strings.Contains(got, "Operator is Davide.") {
		t.Error("missing USER body")
	}
	if strings.Contains(got, "ignore me") {
		t.Error("RANDOM.md should not appear in overlay")
	}
	// Order should follow overlayFiles: SOUL before USER.
	if strings.Index(got, "## SOUL") > strings.Index(got, "## USER") {
		t.Error("SOUL section should appear before USER section")
	}
}

func TestLoadPromptOverlaySkipsBlankFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "SOUL.md"), "   \n\n   ")
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "real content")

	got := LoadPromptOverlay(dir)
	if strings.Contains(got, "## SOUL") {
		t.Error("blank SOUL.md should be skipped, but section appeared")
	}
	if !strings.Contains(got, "## AGENTS") {
		t.Error("expected AGENTS section")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
