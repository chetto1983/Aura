// Package secrets provides SQLite-backed storage for Aura runtime secrets
// (tokens, API keys). The secrets table is intentionally separate from the
// settings table so that a SELECT against settings never accidentally exposes
// a secret value.
package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Canonical secret key names. Callers use these constants; raw string literals
// must never appear at call sites to avoid key-name drift.
const (
	KeyTelegramToken     = "telegram_token"
	KeyLLMAPIKey         = "llm_api_key"
	KeyEmbeddingAPIKey   = "embedding_api_key"
	KeyGarageS3AccessKey = "garage_s3_access_key"
	KeyGarageS3SecretKey = "garage_s3_secret_key"
	KeyQdrantAPIKey      = "qdrant_api_key"
)

// ErrSecretNotFound is returned by Get when the requested key does not exist
// in the secrets table. Callers can use errors.Is to distinguish missing from
// other errors.
var ErrSecretNotFound = errors.New("secrets: key not found")

// Store is the read/write interface for the secrets table.
// List returns only key names — values are never marshalled into the result.
type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]string, error)
}

// SQLiteStore implements Store backed by a *sql.DB.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a Store backed by the provided database connection.
// The caller is responsible for ensuring that migrations have been applied.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// Get returns the value for key. Returns ErrSecretNotFound when the key is
// absent. The error string never contains the secret value.
func (s *SQLiteStore) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM secrets WHERE key = ?`, key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secrets: get %q: %w", key, err)
	}
	return value, nil
}

// Set upserts key→value with the current UTC timestamp. The value is never
// logged.
func (s *SQLiteStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secrets (key, value, updated_at)
         VALUES (?, ?, ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("secrets: set %q: %w", key, err)
	}
	return nil
}

// Delete removes key from the store. It is not an error if the key does not
// exist.
func (s *SQLiteStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("secrets: delete %q: %w", key, err)
	}
	return nil
}

// List returns all stored key names ordered alphabetically. Values are never
// included in the result.
func (s *SQLiteStore) List(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM secrets ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("secrets: list: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("secrets: list scan: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("secrets: list iterate: %w", err)
	}
	return keys, nil
}
