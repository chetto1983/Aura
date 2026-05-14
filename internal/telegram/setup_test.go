package telegram

import (
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/config"
)

func TestSkillSearchRootsIncludeSkillsPathCatalogLayouts(t *testing.T) {
	cfg := &config.Config{
		SkillsPath:              filepath.Join("data", "skills"),
		SkillsInstallProjectDir: "",
	}

	roots := SkillSearchRoots(cfg)
	for _, want := range []string{
		filepath.Join("data", "skills"),
		".agents/skills",
		".claude/skills",
		filepath.Join("data", "skills", ".agents", "skills"),
		filepath.Join("data", "skills", ".claude", "skills"),
		filepath.Join(".", ".agents", "skills"),
		filepath.Join(".", ".claude", "skills"),
	} {
		if !containsSetupTestString(roots, want) {
			t.Fatalf("roots missing %q: %+v", want, roots)
		}
	}
}

func containsSetupTestString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
