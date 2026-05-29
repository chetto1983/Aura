package config

import (
	"testing"
)

func TestLoad_DefaultsApplied(t *testing.T) {
	// Clear any inherited env so we observe the documented defaults.
	t.Setenv("AURA_DB_URL", "")
	t.Setenv("AURA_DB_MIGRATE_URL", "")
	t.Setenv("AURA_RUN_DIR", "")
	t.Setenv("AURA_CONTEXT_PREVIEW_CAP_BYTES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DB.URL != "" {
		t.Errorf("DB.URL: want empty (no env), got %q", cfg.DB.URL)
	}
	if cfg.DB.MigrateURL != "" {
		t.Errorf("DB.MigrateURL: want empty (no env), got %q", cfg.DB.MigrateURL)
	}
	if cfg.RunDir == "" {
		t.Error("RunDir: want a non-empty default path, got empty")
	}
	if cfg.ToolPreviewCap != 2048 {
		t.Errorf("ToolPreviewCap: want 2048 default, got %d", cfg.ToolPreviewCap)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("AURA_DB_URL", "postgres://aura@localhost/aura")
	t.Setenv("AURA_DB_MIGRATE_URL", "postgres://aura_migrate@localhost/aura")
	t.Setenv("AURA_RUN_DIR", "/tmp/aura-test-run")
	t.Setenv("AURA_CONTEXT_PREVIEW_CAP_BYTES", "4096")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DB.URL != "postgres://aura@localhost/aura" {
		t.Errorf("DB.URL override: got %q", cfg.DB.URL)
	}
	if cfg.DB.MigrateURL != "postgres://aura_migrate@localhost/aura" {
		t.Errorf("DB.MigrateURL override: got %q", cfg.DB.MigrateURL)
	}
	if cfg.RunDir != "/tmp/aura-test-run" {
		t.Errorf("RunDir override: got %q", cfg.RunDir)
	}
	if cfg.ToolPreviewCap != 4096 {
		t.Errorf("ToolPreviewCap override: got %d", cfg.ToolPreviewCap)
	}
}

func TestEnvIntDefault_ParsesValid_FallsBackOnGarbage(t *testing.T) {
	t.Setenv("AURA_TEST_INT_VALID", "42")
	if got := envIntDefault("AURA_TEST_INT_VALID", 7); got != 42 {
		t.Errorf("valid int: want 42, got %d", got)
	}

	t.Setenv("AURA_TEST_INT_GARBAGE", "not-a-number")
	if got := envIntDefault("AURA_TEST_INT_GARBAGE", 7); got != 7 {
		t.Errorf("garbage: want fallback 7, got %d", got)
	}

	t.Setenv("AURA_TEST_INT_EMPTY", "")
	if got := envIntDefault("AURA_TEST_INT_EMPTY", 13); got != 13 {
		t.Errorf("empty: want fallback 13, got %d", got)
	}
}

func TestEnvDefault_FallbackOnUnset(t *testing.T) {
	t.Setenv("AURA_TEST_STR_EMPTY", "")
	if got := envDefault("AURA_TEST_STR_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("empty: want fallback, got %q", got)
	}

	t.Setenv("AURA_TEST_STR_SET", "real")
	if got := envDefault("AURA_TEST_STR_SET", "fallback"); got != "real" {
		t.Errorf("set: want 'real', got %q", got)
	}
}
