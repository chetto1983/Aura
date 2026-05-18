package main

import (
	"context"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/secrets"
)

// applySecretsToConfig reads each canonical secret key from store and
// overwrites the corresponding cfg field when the store has a non-empty value.
// Precedence: SQLite secrets table > env var > empty.
// ErrSecretNotFound and any other store errors are treated as "not present":
// the existing cfg field (populated from env by config.Load) is preserved.
func applySecretsToConfig(ctx context.Context, store secrets.Store, cfg *config.Config) {
	override := func(key string, dest *string) {
		val, err := store.Get(ctx, key)
		if err == nil && val != "" {
			*dest = val
		}
	}

	override(secrets.KeyTelegramToken, &cfg.TelegramToken)
	override(secrets.KeyLLMAPIKey, &cfg.LLMAPIKey)
	override(secrets.KeyEmbeddingAPIKey, &cfg.EmbeddingAPIKey)
	override(secrets.KeyGarageS3AccessKey, &cfg.GarageS3AccessKey)
	override(secrets.KeyGarageS3SecretKey, &cfg.GarageS3SecretKey)
	override(secrets.KeyQdrantAPIKey, &cfg.QdrantAPIKey)
	override(secrets.KeyMistralAPIKey, &cfg.MistralAPIKey)
}
