package config

import (
	"strings"
	"testing"
)

// clearPostgresEnv zeroes every Postgres-related env var so each test runs from
// a known baseline regardless of what the host shell sets.
func clearPostgresEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
		"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_SSLMODE",
		"AURA_DB_APP_ROLE", "AURA_DB_MIGRATE_ROLE",
		"AURA_DB_URL", "AURA_DB_MIGRATE_URL", "AURA_DB_BOOTSTRAP_URL",
		"AURA_RUN_DIR", "AURA_CONTEXT_PREVIEW_CAP_BYTES",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// With no password and no override, all DSNs compose to empty.
	if cfg.DB.URL != "" {
		t.Errorf("DB.URL: want empty (no POSTGRES_PASSWORD), got %q", cfg.DB.URL)
	}
	if cfg.DB.MigrateURL != "" {
		t.Errorf("DB.MigrateURL: want empty (no POSTGRES_PASSWORD), got %q", cfg.DB.MigrateURL)
	}
	if cfg.DB.BootstrapURL != "" {
		t.Errorf("DB.BootstrapURL: want empty (no POSTGRES_PASSWORD), got %q", cfg.DB.BootstrapURL)
	}
	if cfg.DB.Password != "" {
		t.Errorf("DB.Password: want empty, got %q", cfg.DB.Password)
	}
	if cfg.RunDir == "" {
		t.Error("RunDir: want a non-empty default path, got empty")
	}
	if cfg.ToolPreviewCap != 2048 {
		t.Errorf("ToolPreviewCap: want 2048 default, got %d", cfg.ToolPreviewCap)
	}
}

func TestLoad_ComposesFromPrimitives(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "s3cret")
	// User/DB/Host/Port/SSLMode all fall back to defaults (aura/aura/127.0.0.1/5432/disable).

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	wantApp := "postgres://aura_app:s3cret@127.0.0.1:5432/aura?sslmode=disable"
	if cfg.DB.URL != wantApp {
		t.Errorf("DB.URL composed: want %q, got %q", wantApp, cfg.DB.URL)
	}
	wantMigrate := "postgres://aura_migrate:s3cret@127.0.0.1:5432/aura?sslmode=disable"
	if cfg.DB.MigrateURL != wantMigrate {
		t.Errorf("DB.MigrateURL composed: want %q, got %q", wantMigrate, cfg.DB.MigrateURL)
	}
	wantBootstrap := "postgres://aura:s3cret@127.0.0.1:5432/aura?sslmode=disable"
	if cfg.DB.BootstrapURL != wantBootstrap {
		t.Errorf("DB.BootstrapURL composed: want %q, got %q", wantBootstrap, cfg.DB.BootstrapURL)
	}
	if cfg.DB.Password != "s3cret" {
		t.Errorf("DB.Password: want %q, got %q", "s3cret", cfg.DB.Password)
	}
}

func TestLoad_FullDSNOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "ignored-because-of-override")
	t.Setenv("AURA_DB_URL", "postgres://prod_app:prod_pwd@db.prod:5432/aura?sslmode=require")
	t.Setenv("AURA_DB_MIGRATE_URL", "postgres://prod_migrate:prod_pwd@db.prod:5432/aura?sslmode=require")
	t.Setenv("AURA_DB_BOOTSTRAP_URL", "postgres://prod_super:prod_pwd@db.prod:5432/aura?sslmode=require")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(cfg.DB.URL, "prod_app") {
		t.Errorf("DB.URL: override not applied, got %q", cfg.DB.URL)
	}
	if !strings.Contains(cfg.DB.MigrateURL, "prod_migrate") {
		t.Errorf("DB.MigrateURL: override not applied, got %q", cfg.DB.MigrateURL)
	}
	if !strings.Contains(cfg.DB.BootstrapURL, "prod_super") {
		t.Errorf("DB.BootstrapURL: override not applied, got %q", cfg.DB.BootstrapURL)
	}
}

func TestLoad_PrimitivesAndRoleOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_USER", "supr")
	t.Setenv("POSTGRES_PASSWORD", "p@ss/word") // includes URL-significant chars
	t.Setenv("POSTGRES_DB", "auradb")
	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PORT", "6432")
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("AURA_DB_APP_ROLE", "myapp")
	t.Setenv("AURA_DB_MIGRATE_ROLE", "mymigrate")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// Password chars must be URL-escaped (/ → %2F, @ stays raw since it's after colon).
	if !strings.Contains(cfg.DB.URL, "myapp:p%40ss%2Fword@db.internal:6432/auradb?sslmode=require") {
		t.Errorf("DB.URL composition mishandled special chars: %q", cfg.DB.URL)
	}
	if !strings.Contains(cfg.DB.MigrateURL, "mymigrate:") {
		t.Errorf("DB.MigrateURL did not use AURA_DB_MIGRATE_ROLE: %q", cfg.DB.MigrateURL)
	}
	if !strings.Contains(cfg.DB.BootstrapURL, "supr:") {
		t.Errorf("DB.BootstrapURL did not use POSTGRES_USER: %q", cfg.DB.BootstrapURL)
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
