package config

import "testing"

func TestLoadDBAssetObjectStoreDefaultsAndOverrides(t *testing.T) {
	clearPostgresEnv(t)

	cfg := LoadDB()
	if cfg.ObjectStoreBackend != "garage" {
		t.Errorf("ObjectStoreBackend default = %q, want garage", cfg.ObjectStoreBackend)
	}
	if cfg.ObjectStoreEndpoint != "http://127.0.0.1:3900" {
		t.Errorf("ObjectStoreEndpoint default = %q, want http://127.0.0.1:3900", cfg.ObjectStoreEndpoint)
	}
	if cfg.ObjectStorePublicEndpoint != "" {
		t.Errorf("ObjectStorePublicEndpoint default = %q, want empty", cfg.ObjectStorePublicEndpoint)
	}
	if cfg.ObjectStoreRegion != "garage" {
		t.Errorf("ObjectStoreRegion default = %q, want garage", cfg.ObjectStoreRegion)
	}
	if cfg.ObjectStoreBucket != "aura-assets" {
		t.Errorf("ObjectStoreBucket default = %q, want aura-assets", cfg.ObjectStoreBucket)
	}
	if cfg.ObjectStoreAccessKey != "" {
		t.Errorf("ObjectStoreAccessKey default = %q, want empty", cfg.ObjectStoreAccessKey)
	}
	if cfg.ObjectStoreSecretKey != "" {
		t.Errorf("ObjectStoreSecretKey default = %q, want empty", cfg.ObjectStoreSecretKey)
	}
	if !cfg.ObjectStorePathStyle {
		t.Error("ObjectStorePathStyle default = false, want true")
	}
	if cfg.AssetMaxDocumentBytes != 104857600 {
		t.Errorf("AssetMaxDocumentBytes default = %d, want 104857600", cfg.AssetMaxDocumentBytes)
	}
	if cfg.AssetMaxImageBytes != 26214400 {
		t.Errorf("AssetMaxImageBytes default = %d, want 26214400", cfg.AssetMaxImageBytes)
	}
	if cfg.AssetMaxAudioBytes != 104857600 {
		t.Errorf("AssetMaxAudioBytes default = %d, want 104857600", cfg.AssetMaxAudioBytes)
	}
	if cfg.AssetPresignTTLSec != 600 {
		t.Errorf("AssetPresignTTLSec default = %d, want 600", cfg.AssetPresignTTLSec)
	}
	if cfg.AssetProcessingConcurrent != 2 {
		t.Errorf("AssetProcessingConcurrent default = %d, want 2", cfg.AssetProcessingConcurrent)
	}
	if cfg.TelegramAPIBaseURL != "" {
		t.Errorf("TelegramAPIBaseURL default = %q, want empty", cfg.TelegramAPIBaseURL)
	}
	if cfg.TelegramFileBaseURL != "" {
		t.Errorf("TelegramFileBaseURL default = %q, want empty", cfg.TelegramFileBaseURL)
	}
	if cfg.TelegramLocalBotAPI {
		t.Error("TelegramLocalBotAPI default = true, want false")
	}

	t.Setenv("AURA_OBJECTSTORE_BACKEND", "filesystem-dev")
	t.Setenv("AURA_OBJECTSTORE_ENDPOINT", "file:///tmp/aura-assets")
	t.Setenv("AURA_OBJECTSTORE_PUBLIC_ENDPOINT", "https://assets.example.test")
	t.Setenv("AURA_OBJECTSTORE_REGION", "local")
	t.Setenv("AURA_OBJECTSTORE_BUCKET", "custom-assets")
	t.Setenv("AURA_OBJECTSTORE_ACCESS_KEY", "access")
	t.Setenv("AURA_OBJECTSTORE_SECRET_KEY", "secret")
	t.Setenv("AURA_OBJECTSTORE_PATH_STYLE", "false")
	t.Setenv("AURA_ASSET_MAX_DOCUMENT_BYTES", "123")
	t.Setenv("AURA_ASSET_MAX_IMAGE_BYTES", "456")
	t.Setenv("AURA_ASSET_MAX_AUDIO_BYTES", "789")
	t.Setenv("AURA_ASSET_PRESIGN_TTL_SEC", "42")
	t.Setenv("AURA_ASSET_PROCESSING_CONCURRENCY", "3")
	t.Setenv("TELEGRAM_API_BASE_URL", "http://telegram-api:8081")
	t.Setenv("TELEGRAM_FILE_BASE_URL", "http://telegram-api:8081/file")
	t.Setenv("AURA_TELEGRAM_LOCAL_BOT_API", "true")

	cfg = LoadDB()
	if cfg.ObjectStoreBackend != "filesystem-dev" {
		t.Errorf("ObjectStoreBackend override = %q, want filesystem-dev", cfg.ObjectStoreBackend)
	}
	if cfg.ObjectStoreEndpoint != "file:///tmp/aura-assets" {
		t.Errorf("ObjectStoreEndpoint override = %q, want file:///tmp/aura-assets", cfg.ObjectStoreEndpoint)
	}
	if cfg.ObjectStorePublicEndpoint != "https://assets.example.test" {
		t.Errorf("ObjectStorePublicEndpoint override = %q, want https://assets.example.test", cfg.ObjectStorePublicEndpoint)
	}
	if cfg.ObjectStoreRegion != "local" {
		t.Errorf("ObjectStoreRegion override = %q, want local", cfg.ObjectStoreRegion)
	}
	if cfg.ObjectStoreBucket != "custom-assets" {
		t.Errorf("ObjectStoreBucket override = %q, want custom-assets", cfg.ObjectStoreBucket)
	}
	if cfg.ObjectStoreAccessKey != "access" {
		t.Errorf("ObjectStoreAccessKey override = %q, want access", cfg.ObjectStoreAccessKey)
	}
	if cfg.ObjectStoreSecretKey != "secret" {
		t.Errorf("ObjectStoreSecretKey override = %q, want secret", cfg.ObjectStoreSecretKey)
	}
	if cfg.ObjectStorePathStyle {
		t.Error("ObjectStorePathStyle override = true, want false")
	}
	if cfg.AssetMaxDocumentBytes != 123 || cfg.AssetMaxImageBytes != 456 || cfg.AssetMaxAudioBytes != 789 {
		t.Errorf("asset size overrides = %d/%d/%d, want 123/456/789", cfg.AssetMaxDocumentBytes, cfg.AssetMaxImageBytes, cfg.AssetMaxAudioBytes)
	}
	if cfg.AssetPresignTTLSec != 42 {
		t.Errorf("AssetPresignTTLSec override = %d, want 42", cfg.AssetPresignTTLSec)
	}
	if cfg.AssetProcessingConcurrent != 3 {
		t.Errorf("AssetProcessingConcurrent override = %d, want 3", cfg.AssetProcessingConcurrent)
	}
	if cfg.TelegramAPIBaseURL != "http://telegram-api:8081" {
		t.Errorf("TelegramAPIBaseURL override = %q, want http://telegram-api:8081", cfg.TelegramAPIBaseURL)
	}
	if cfg.TelegramFileBaseURL != "http://telegram-api:8081/file" {
		t.Errorf("TelegramFileBaseURL override = %q, want http://telegram-api:8081/file", cfg.TelegramFileBaseURL)
	}
	if !cfg.TelegramLocalBotAPI {
		t.Error("TelegramLocalBotAPI override = false, want true")
	}
}
