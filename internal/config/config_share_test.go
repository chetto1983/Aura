package config

import "testing"

// TestShareConfig asserts the Phase 37F AURA_SHARE_* operator surface (WEBSHARE-02/03):
// loadShare() parses both knobs into ShareConfig, fails closed on unset/malformed values,
// and clamps a non-positive MaxExpiryDays back to the 90-day default. Lives in its own file
// so config_test.go stays under the 600-LOC cap (CLAUDE.md NO GOD CLASS).
func TestShareConfig(t *testing.T) {
	t.Run("PublicEnabled unset defaults false", func(t *testing.T) {
		t.Setenv("AURA_SHARE_PUBLIC_ENABLED", "")
		cfg := loadShare()
		if cfg.PublicEnabled {
			t.Errorf("PublicEnabled unset: want false, got true")
		}
	})

	t.Run("PublicEnabled=true parses true", func(t *testing.T) {
		t.Setenv("AURA_SHARE_PUBLIC_ENABLED", "true")
		cfg := loadShare()
		if !cfg.PublicEnabled {
			t.Errorf("PublicEnabled=true: want true, got false")
		}
	})

	t.Run("PublicEnabled=garbage falls back to false", func(t *testing.T) {
		t.Setenv("AURA_SHARE_PUBLIC_ENABLED", "garbage")
		cfg := loadShare()
		if cfg.PublicEnabled {
			t.Errorf("PublicEnabled=garbage: want silent fallback false, got true")
		}
	})

	t.Run("MaxExpiryDays unset defaults 90", func(t *testing.T) {
		t.Setenv("AURA_SHARE_MAX_EXPIRY_DAYS", "")
		cfg := loadShare()
		if cfg.MaxExpiryDays != 90 {
			t.Errorf("MaxExpiryDays unset: want 90, got %d", cfg.MaxExpiryDays)
		}
	})

	t.Run("MaxExpiryDays=30 parses 30", func(t *testing.T) {
		t.Setenv("AURA_SHARE_MAX_EXPIRY_DAYS", "30")
		cfg := loadShare()
		if cfg.MaxExpiryDays != 30 {
			t.Errorf("MaxExpiryDays=30: want 30, got %d", cfg.MaxExpiryDays)
		}
	})

	t.Run("MaxExpiryDays=garbage falls back to 90", func(t *testing.T) {
		t.Setenv("AURA_SHARE_MAX_EXPIRY_DAYS", "garbage")
		cfg := loadShare()
		if cfg.MaxExpiryDays != 90 {
			t.Errorf("MaxExpiryDays=garbage: want silent fallback 90, got %d", cfg.MaxExpiryDays)
		}
	})

	t.Run("MaxExpiryDays=0 and negative clamp to 90", func(t *testing.T) {
		for _, v := range []string{"0", "-1", "-30"} {
			t.Setenv("AURA_SHARE_MAX_EXPIRY_DAYS", v)
			cfg := loadShare()
			if cfg.MaxExpiryDays != 90 {
				t.Errorf("MaxExpiryDays=%s: want clamped 90, got %d", v, cfg.MaxExpiryDays)
			}
		}
	})
}
