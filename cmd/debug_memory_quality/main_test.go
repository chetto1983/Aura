package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/settings"
)

func TestLoadLiveConfigAppliesDashboardSettings(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "aura.db")
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("LLM_API_KEY=env-key\nLLM_BASE_URL=https://env.example/v1\nLLM_MODEL=env-model\nDB_PATH="+dbPath+"\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("AURA_ENV_PATH", envPath)
	t.Setenv("DB_PATH", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")

	store, err := settings.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	if err := store.Set(context.Background(), settings.KeyLLMBaseURL, "https://db.example/v1"); err != nil {
		t.Fatalf("set base url: %v", err)
	}
	if err := store.Set(context.Background(), settings.KeyLLMModel, "db-model"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close settings: %v", err)
	}

	cfg, err := loadLiveConfig(context.Background())
	if err != nil {
		t.Fatalf("loadLiveConfig: %v", err)
	}
	if cfg.LLMAPIKey != "env-key" {
		t.Fatalf("LLMAPIKey = %q, want env-key", cfg.LLMAPIKey)
	}
	if cfg.LLMBaseURL != "https://db.example/v1" {
		t.Fatalf("LLMBaseURL = %q, want DB override", cfg.LLMBaseURL)
	}
	if cfg.LLMModel != "db-model" {
		t.Fatalf("LLMModel = %q, want DB override", cfg.LLMModel)
	}
}
