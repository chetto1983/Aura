package config

import (
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/profile"
)

func TestProfileConfigDefaultsAndOverrides(t *testing.T) {
	clearPostgresEnv(t)

	cfg := LoadDB()
	if cfg.ProfileDir != profile.DefaultRoot() {
		t.Errorf("ProfileDir default = %q, want %q", cfg.ProfileDir, profile.DefaultRoot())
	}
	if cfg.ProfileCertaintyN != 3 {
		t.Errorf("ProfileCertaintyN default = %d, want 3", cfg.ProfileCertaintyN)
	}

	root := filepath.Join(t.TempDir(), "agents")
	t.Setenv("AURA_PROFILE_DIR", root)
	t.Setenv("AURA_PROFILE_CERTAINTY_N", "5")
	cfg = LoadDB()
	if cfg.ProfileDir != root {
		t.Errorf("ProfileDir override = %q, want %q", cfg.ProfileDir, root)
	}
	if cfg.ProfileCertaintyN != 5 {
		t.Errorf("ProfileCertaintyN override = %d, want 5", cfg.ProfileCertaintyN)
	}

	t.Setenv("AURA_PROFILE_CERTAINTY_N", "not-a-number")
	cfg = LoadDB()
	if cfg.ProfileCertaintyN != 3 {
		t.Errorf("ProfileCertaintyN malformed fallback = %d, want 3", cfg.ProfileCertaintyN)
	}
}
