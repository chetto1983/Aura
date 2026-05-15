package main

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/secrets"
)

// stubSecretsStore is an in-memory secrets.Store for testing applySecretsToConfig.
type stubSecretsStore struct {
	data map[string]string
}

func (s *stubSecretsStore) Get(_ context.Context, key string) (string, error) {
	if v, ok := s.data[key]; ok {
		return v, nil
	}
	return "", secrets.ErrSecretNotFound
}

func (s *stubSecretsStore) Set(_ context.Context, key, value string) error {
	s.data[key] = value
	return nil
}

func (s *stubSecretsStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func (s *stubSecretsStore) List(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestApplySecretsToConfig_SQLiteWinsOverEnv(t *testing.T) {
	store := &stubSecretsStore{data: map[string]string{
		secrets.KeyTelegramToken:     "sqlite-token",
		secrets.KeyLLMAPIKey:         "sqlite-llm",
		secrets.KeyEmbeddingAPIKey:   "sqlite-embed",
		secrets.KeyGarageS3AccessKey: "sqlite-garage-access",
		secrets.KeyGarageS3SecretKey: "sqlite-garage-secret",
		secrets.KeyQdrantAPIKey:      "sqlite-qdrant",
	}}
	cfg := &config.Config{
		TelegramToken:     "env-token",
		LLMAPIKey:         "env-llm",
		EmbeddingAPIKey:   "env-embed",
		GarageS3AccessKey: "env-garage-access",
		GarageS3SecretKey: "env-garage-secret",
		QdrantAPIKey:      "env-qdrant",
	}

	applySecretsToConfig(context.Background(), store, cfg)

	if cfg.TelegramToken != "sqlite-token" {
		t.Errorf("TelegramToken = %q, want sqlite-token", cfg.TelegramToken)
	}
	if cfg.LLMAPIKey != "sqlite-llm" {
		t.Errorf("LLMAPIKey = %q, want sqlite-llm", cfg.LLMAPIKey)
	}
	if cfg.EmbeddingAPIKey != "sqlite-embed" {
		t.Errorf("EmbeddingAPIKey = %q, want sqlite-embed", cfg.EmbeddingAPIKey)
	}
	if cfg.GarageS3AccessKey != "sqlite-garage-access" {
		t.Errorf("GarageS3AccessKey = %q, want sqlite-garage-access", cfg.GarageS3AccessKey)
	}
	if cfg.GarageS3SecretKey != "sqlite-garage-secret" {
		t.Errorf("GarageS3SecretKey = %q, want sqlite-garage-secret", cfg.GarageS3SecretKey)
	}
	if cfg.QdrantAPIKey != "sqlite-qdrant" {
		t.Errorf("QdrantAPIKey = %q, want sqlite-qdrant", cfg.QdrantAPIKey)
	}
}

func TestApplySecretsToConfig_EmptyStorePreservesEnv(t *testing.T) {
	store := &stubSecretsStore{data: map[string]string{}}
	cfg := &config.Config{
		TelegramToken:   "env-token",
		LLMAPIKey:       "env-llm",
		EmbeddingAPIKey: "env-embed",
		QdrantAPIKey:    "env-qdrant",
	}

	applySecretsToConfig(context.Background(), store, cfg)

	if cfg.TelegramToken != "env-token" {
		t.Errorf("TelegramToken = %q, want env-token (env preserved when not in SQLite)", cfg.TelegramToken)
	}
	if cfg.LLMAPIKey != "env-llm" {
		t.Errorf("LLMAPIKey = %q, want env-llm (env preserved when not in SQLite)", cfg.LLMAPIKey)
	}
	if cfg.EmbeddingAPIKey != "env-embed" {
		t.Errorf("EmbeddingAPIKey = %q, want env-embed (env preserved when not in SQLite)", cfg.EmbeddingAPIKey)
	}
	if cfg.QdrantAPIKey != "env-qdrant" {
		t.Errorf("QdrantAPIKey = %q, want env-qdrant (env preserved when not in SQLite)", cfg.QdrantAPIKey)
	}
}

// TestApplySecretsToConfig_EmptyStoreAndEnvLeavesEmpty verifies that on a
// fresh install (empty SQLite secrets table, no env vars set) cfg.TelegramToken
// stays empty so IsBootstrapped() returns false and the setup wizard triggers.
func TestApplySecretsToConfig_EmptyStoreAndEnvLeavesEmpty(t *testing.T) {
	store := &stubSecretsStore{data: map[string]string{}}
	cfg := &config.Config{} // TelegramToken == ""

	applySecretsToConfig(context.Background(), store, cfg)

	if cfg.TelegramToken != "" {
		t.Errorf("TelegramToken = %q, want empty (wizard must be triggered on fresh install)", cfg.TelegramToken)
	}
}

// TestApplySecretsToConfig_PartialStore verifies that SQLite wins for keys it
// has while env values are preserved for keys absent from SQLite.
func TestApplySecretsToConfig_PartialStore(t *testing.T) {
	store := &stubSecretsStore{data: map[string]string{
		secrets.KeyTelegramToken: "sqlite-token",
		// LLMAPIKey intentionally absent from SQLite
	}}
	cfg := &config.Config{
		TelegramToken: "env-token",
		LLMAPIKey:     "env-llm",
	}

	applySecretsToConfig(context.Background(), store, cfg)

	if cfg.TelegramToken != "sqlite-token" {
		t.Errorf("TelegramToken = %q, want sqlite-token (SQLite wins)", cfg.TelegramToken)
	}
	if cfg.LLMAPIKey != "env-llm" {
		t.Errorf("LLMAPIKey = %q, want env-llm (env preserved when key absent from SQLite)", cfg.LLMAPIKey)
	}
}
