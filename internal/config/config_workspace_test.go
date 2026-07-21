package config

import (
	"path/filepath"
	"testing"
)

// TestWorkspaceDir_DefaultAndOverride asserts Amendment #88's WorkspaceDir field:
// an unset AURA_WORKSPACE_DIR resolves to a non-empty absolute path (the
// ~/.aura/workspace code default), and a set value is honored verbatim (the
// in-container /workspace deployment default flows through env, not code).
func TestWorkspaceDir_DefaultAndOverride(t *testing.T) {
	t.Setenv("AURA_WORKSPACE_DIR", "")
	got := LoadDB().WorkspaceDir
	if got == "" || !filepath.IsAbs(got) {
		t.Fatalf("default WorkspaceDir must be a non-empty absolute path, got %q", got)
	}
	t.Setenv("AURA_WORKSPACE_DIR", "/workspace")
	if LoadDB().WorkspaceDir != "/workspace" {
		t.Fatalf("AURA_WORKSPACE_DIR override not honored")
	}
}
