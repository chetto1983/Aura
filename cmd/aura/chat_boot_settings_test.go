package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

type fakeSecretReader map[string]string

func (f fakeSecretReader) Secret(_ context.Context, key string) (string, error) { return f[key], nil }

type failingSecretReader struct{}

func (failingSecretReader) Secret(context.Context, string) (string, error) {
	return "", errors.New("decrypt failed")
}

func TestApplySecretSettingsLetsTheStoredKeysWin(t *testing.T) {
	cfg := &config.Config{OpenRouterManagementKey: "from-env"}
	cfg.LLM.APIKey = "from-env"
	secrets := fakeSecretReader{"OPENROUTER_API_KEY": "sk-services", "AURA_OPENROUTER_MANAGEMENT_KEY": "sk-management"}
	if err := applySecretSettings(context.Background(), secrets, cfg); err != nil {
		t.Fatalf("applySecretSettings: %v", err)
	}
	if cfg.LLM.APIKey != "sk-services" || cfg.OpenRouterManagementKey != "sk-management" {
		t.Fatal("the stored keys did not replace the environment's")
	}
}

func TestApplySecretSettingsKeepsTheEnvironmentWhenNothingIsStored(t *testing.T) {
	cfg := &config.Config{OpenRouterManagementKey: "from-env"}
	cfg.LLM.APIKey = "from-env"
	if err := applySecretSettings(context.Background(), fakeSecretReader{}, cfg); err != nil {
		t.Fatalf("applySecretSettings: %v", err)
	}
	if cfg.LLM.APIKey != "from-env" || cfg.OpenRouterManagementKey != "from-env" {
		t.Fatal("an empty store erased the environment's keys")
	}
}

func TestApplySecretSettingsReportsAnUnreadableSecret(t *testing.T) {
	if err := applySecretSettings(context.Background(), failingSecretReader{}, &config.Config{}); err == nil {
		t.Fatal("an unreadable secret passed silently")
	}
}
