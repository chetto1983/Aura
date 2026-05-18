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
	mustWrite(t, filepath.Join(dir, "AGENT.md"), "runtime notes are file-readable only")
	mustWrite(t, filepath.Join(dir, "USER.md"), "Operator is Davide.")
	mustWrite(t, filepath.Join(dir, "TOOLS.md"), "Use tools carefully.")
	mustWrite(t, filepath.Join(dir, "RANDOM.md"), "ignore me")
	mustWrite(t, filepath.Join(dir, "AGENTS.md"), "dev-only instructions")

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
	if !strings.Contains(got, "## TOOLS") {
		t.Error("missing TOOLS section")
	}
	if !strings.Contains(got, "Use tools carefully.") {
		t.Error("missing TOOLS body")
	}
	if strings.Contains(got, "ignore me") {
		t.Error("RANDOM.md should not appear in overlay")
	}
	if strings.Contains(got, "runtime notes are file-readable only") {
		t.Error("AGENT.md should not be injected into the system prompt")
	}
	if strings.Contains(got, "dev-only instructions") {
		t.Error("AGENTS.md should not appear in runtime overlay")
	}
	// Order should follow overlayFiles: SOUL before USER.
	if strings.Index(got, "## SOUL") > strings.Index(got, "## USER") {
		t.Error("SOUL section should appear before USER section")
	}
}

func TestLoadPromptOverlaySkipsBlankFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "SOUL.md"), "   \n\n   ")
	mustWrite(t, filepath.Join(dir, "USER.md"), "real content")

	got := LoadPromptOverlay(dir)
	if strings.Contains(got, "## SOUL") {
		t.Error("blank SOUL.md should be skipped, but section appeared")
	}
	if !strings.Contains(got, "## USER") {
		t.Error("expected USER section")
	}
}

func TestEnsurePromptOverlayDefaultsCreatesSoulAndToolsOnly(t *testing.T) {
	dir := t.TempDir()

	if err := EnsurePromptOverlayDefaults(dir); err != nil {
		t.Fatalf("EnsurePromptOverlayDefaults() error = %v", err)
	}

	for _, name := range []string{"SOUL.md", "TOOLS.md"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("expected %s to be created: %v", name, err)
		}
		if strings.TrimSpace(string(body)) == "" {
			t.Fatalf("%s is blank", name)
		}
	}
	for _, name := range []string{"AGENT.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be created, err=%v", name, err)
		}
	}
}

func TestEnsurePromptOverlayDefaultsDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	mustWrite(t, path, "custom soul")

	if err := EnsurePromptOverlayDefaults(dir); err != nil {
		t.Fatalf("EnsurePromptOverlayDefaults() error = %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "custom soul" {
		t.Fatalf("SOUL.md overwritten: %q", body)
	}
}

func TestIsOverlayFileName(t *testing.T) {
	watched := []string{"SOUL.md", "USER.md", "TOOLS.md", "OPS.md", "AGENT.md"}
	for _, name := range watched {
		if !IsOverlayFileName(name) {
			t.Errorf("expected %s to be an overlay watched file", name)
		}
	}
	notWatched := []string{"RANDOM.md", "tools.md", "agent.md", "AGENTS.md", ""}
	for _, name := range notWatched {
		if IsOverlayFileName(name) {
			t.Errorf("expected %s to NOT be an overlay watched file", name)
		}
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
