// Package secrets provides SQLite-backed storage for Aura runtime secrets.
// This file contains the one-shot .env → SQLite migration helper.
package secrets

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// envVarToSecretKey maps retired .env variable names to canonical secret key
// constants. Unknown env-var names are silently skipped during migration.
var envVarToSecretKey = map[string]string{
	"TELEGRAM_TOKEN":       KeyTelegramToken,
	"LLM_API_KEY":          KeyLLMAPIKey,
	"EMBEDDING_API_KEY":    KeyEmbeddingAPIKey,
	"GARAGE_S3_ACCESS_KEY": KeyGarageS3AccessKey,
	"GARAGE_S3_SECRET_KEY": KeyGarageS3SecretKey,
	"QDRANT_API_KEY":       KeyQdrantAPIKey,
}

// ImportFromDotEnv reads the .env file at dotenvPath and imports any recognised
// secret keys into store. It is idempotent: keys already present in the store
// are skipped (the SQLite value wins on conflict). The returned slice contains
// only the canonical secret key names that were actually written — values are
// never included. A missing .env file is not an error; it returns (nil, nil).
// Empty env values are silently skipped. Unknown env-var names are silently
// skipped.
func ImportFromDotEnv(ctx context.Context, store Store, dotenvPath string, logger *slog.Logger) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	parsed, err := parseDotEnvForMigration(dotenvPath)
	if err != nil {
		return nil, fmt.Errorf("secrets: read %q: %w", dotenvPath, err)
	}
	if parsed == nil {
		// File did not exist — not an error.
		return nil, nil
	}

	var imported []string
	for envVar, secretKey := range envVarToSecretKey {
		value, ok := parsed[envVar]
		if !ok || value == "" {
			continue
		}

		// Idempotency check: skip if the key is already stored.
		_, err := store.Get(ctx, secretKey)
		if err == nil {
			// Already present — SQLite wins; do not overwrite.
			continue
		}
		if !errors.Is(err, ErrSecretNotFound) {
			return imported, fmt.Errorf("secrets: check %q before import: %w", secretKey, err)
		}

		if err := store.Set(ctx, secretKey, value); err != nil {
			return imported, err
		}
		imported = append(imported, secretKey)
	}

	if len(imported) > 0 {
		logger.Info("secrets: imported from .env", "keys", imported, "count", len(imported))
	} else {
		logger.Info("secrets: .env migration: nothing to import", "path", dotenvPath)
	}

	return imported, nil
}

// parseDotEnvForMigration reads KEY=VALUE pairs from path into a map without
// modifying process environment. Returns nil (not an error) when the file does
// not exist. Blank lines and comment lines (starting with #) are skipped.
// Surrounding single/double quotes are stripped from values.
func parseDotEnvForMigration(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			result[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
