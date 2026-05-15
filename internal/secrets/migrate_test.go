package secrets_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/secrets"
	"github.com/aura/aura/internal/testutil"
)

// silentLogger returns a slog.Logger that discards all output.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writeTempDotEnv writes content to a temporary .env file and returns its path.
func writeTempDotEnv(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp .env: %v", err)
	}
	return path
}

func openMigratedStoreForMigrate(t *testing.T) *secrets.SQLiteStore {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	return secrets.NewSQLiteStore(db)
}

func TestImportFromDotEnvHappyPath(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	dotenv := writeTempDotEnv(t, `
TELEGRAM_TOKEN=bot:12345:abcDEFGHIJKLMNOP
LLM_API_KEY=sk-test-llm
EMBEDDING_API_KEY=sk-test-embed
GARAGE_S3_ACCESS_KEY=GKaccesskey
GARAGE_S3_SECRET_KEY=GKsecretkey
QDRANT_API_KEY=qdrant-api-key
UNKNOWN_VAR=should_be_skipped
`)

	imported, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}

	wantKeys := []string{
		secrets.KeyTelegramToken,
		secrets.KeyLLMAPIKey,
		secrets.KeyEmbeddingAPIKey,
		secrets.KeyGarageS3AccessKey,
		secrets.KeyGarageS3SecretKey,
		secrets.KeyQdrantAPIKey,
	}
	if len(imported) != len(wantKeys) {
		t.Fatalf("imported %d keys, want %d; got %v", len(imported), len(wantKeys), imported)
	}
	for _, k := range wantKeys {
		if !slices.Contains(imported, k) {
			t.Errorf("expected key %q in imported list; got %v", k, imported)
		}
	}

	// Verify all values were stored correctly.
	got, err := store.Get(ctx, secrets.KeyTelegramToken)
	if err != nil {
		t.Fatalf("Get telegram token: %v", err)
	}
	if got != "bot:12345:abcDEFGHIJKLMNOP" {
		t.Errorf("telegram token = %q, want %q", got, "bot:12345:abcDEFGHIJKLMNOP")
	}
}

func TestImportFromDotEnvIdempotent(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	// Pre-populate one key so it is already in the store.
	if err := store.Set(ctx, secrets.KeyTelegramToken, "existing-token"); err != nil {
		t.Fatalf("Set pre-existing: %v", err)
	}

	dotenv := writeTempDotEnv(t, `
TELEGRAM_TOKEN=new-token-should-be-ignored
LLM_API_KEY=sk-fresh
`)

	imported, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}

	// Only LLM_API_KEY should be imported; TELEGRAM_TOKEN is already present.
	if slices.Contains(imported, secrets.KeyTelegramToken) {
		t.Errorf("KeyTelegramToken must not be imported when already present; imported=%v", imported)
	}
	if !slices.Contains(imported, secrets.KeyLLMAPIKey) {
		t.Errorf("KeyLLMAPIKey should have been imported; imported=%v", imported)
	}

	// The existing token must not have been overwritten.
	got, err := store.Get(ctx, secrets.KeyTelegramToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "existing-token" {
		t.Errorf("telegram token overwritten: got %q, want %q", got, "existing-token")
	}

	// Second full run is a complete no-op.
	imported2, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("second ImportFromDotEnv: %v", err)
	}
	if len(imported2) != 0 {
		t.Errorf("second run should import nothing; got %v", imported2)
	}
}

func TestImportFromDotEnvEmptyValuesSkipped(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	dotenv := writeTempDotEnv(t, `
TELEGRAM_TOKEN=
LLM_API_KEY=sk-real
`)

	imported, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}

	// Empty TELEGRAM_TOKEN must be skipped.
	if slices.Contains(imported, secrets.KeyTelegramToken) {
		t.Errorf("empty TELEGRAM_TOKEN should be skipped; imported=%v", imported)
	}
	// Non-empty LLM_API_KEY must be imported.
	if !slices.Contains(imported, secrets.KeyLLMAPIKey) {
		t.Errorf("non-empty LLM_API_KEY should be imported; imported=%v", imported)
	}

	// Verify the empty key was not written to the store.
	_, err = store.Get(ctx, secrets.KeyTelegramToken)
	if err == nil {
		t.Error("empty TELEGRAM_TOKEN must not be stored")
	}
}

func TestImportFromDotEnvMissingFileIsNotError(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	nonExistent := filepath.Join(t.TempDir(), "no_such.env")

	imported, err := secrets.ImportFromDotEnv(ctx, store, nonExistent, silentLogger())
	if err != nil {
		t.Fatalf("missing file must not be an error; got: %v", err)
	}
	if len(imported) != 0 {
		t.Errorf("missing file must import nothing; got %v", imported)
	}
}

func TestImportFromDotEnvValuesWithPunctuation(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	// Telebot tokens look like "bot123456789:AbCdEfGhIjKlMnOpQrStUvWxYz-_abc123"
	// They contain colons, hyphens, underscores, mixed case — must survive roundtrip.
	token := "bot123456789:AbCdEfGhIjKlMnOpQrStUvWxYz-_abc123"

	dotenv := writeTempDotEnv(t, "TELEGRAM_TOKEN="+token+"\n")

	imported, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}
	if !slices.Contains(imported, secrets.KeyTelegramToken) {
		t.Fatalf("KeyTelegramToken not in imported=%v", imported)
	}

	got, err := store.Get(ctx, secrets.KeyTelegramToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != token {
		t.Errorf("token roundtrip failed: got %q, want %q", got, token)
	}
}

func TestImportFromDotEnvQuotedValuesStripped(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	dotenv := writeTempDotEnv(t, `
TELEGRAM_TOKEN="bot:123:abc"
LLM_API_KEY='sk-test'
`)

	_, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}

	got, _ := store.Get(ctx, secrets.KeyTelegramToken)
	if got != "bot:123:abc" {
		t.Errorf("double-quoted token = %q, want %q", got, "bot:123:abc")
	}

	got2, _ := store.Get(ctx, secrets.KeyLLMAPIKey)
	if got2 != "sk-test" {
		t.Errorf("single-quoted key = %q, want %q", got2, "sk-test")
	}
}

func TestImportFromDotEnvUnknownEnvVarsSkipped(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	dotenv := writeTempDotEnv(t, `
UNKNOWN_APP_KEY=some_value
HTTP_PORT=8080
DB_PATH=/data/aura.db
TELEGRAM_TOKEN=bot:real:token
`)

	imported, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}
	if len(imported) != 1 {
		t.Errorf("expected exactly 1 key imported; got %v", imported)
	}
	if !slices.Contains(imported, secrets.KeyTelegramToken) {
		t.Errorf("expected KeyTelegramToken imported; got %v", imported)
	}
}

func TestImportFromDotEnvReturnedSliceContainsNoValues(t *testing.T) {
	store := openMigratedStoreForMigrate(t)
	ctx := context.Background()

	sensitiveValue := "SUPER_SECRET_VALUE_MUST_NOT_APPEAR"
	dotenv := writeTempDotEnv(t, "TELEGRAM_TOKEN="+sensitiveValue+"\n")

	imported, err := secrets.ImportFromDotEnv(ctx, store, dotenv, silentLogger())
	if err != nil {
		t.Fatalf("ImportFromDotEnv: %v", err)
	}
	for _, item := range imported {
		if item == sensitiveValue {
			t.Errorf("imported slice contains a secret value: %q", item)
		}
	}
}
