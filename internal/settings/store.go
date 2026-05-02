// Package settings persists user-tunable configuration in SQLite so the
// dashboard can edit it at runtime instead of forcing the operator to
// hand-edit .env and restart.
//
// Threat model and scope (slice 14a):
//   - Holds non-secret tunables (budgets, model choices, paths, feature
//     flags) and operator-rotated secrets (LLM_API_KEY, EMBEDDING_API_KEY,
//     MISTRAL_API_KEY, OLLAMA_API_KEY). The single hard exclusion is
//     TELEGRAM_TOKEN — it's the bootstrap secret, must be present before
//     the bot can answer /setup, and stays in .env.
//   - Values are persisted in plain text. Treat the SQLite file like .env:
//     OS-level file permissions are the security boundary.
//   - Reads are env-then-DB (DB wins) for everything Applier touches.
//     This lets an operator wipe the DB and fall back to .env without
//     losing access to the bot.
//   - Uses the same SQLite file as auth/scheduler (cfg.DBPath) so backups
//     stay one artifact.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned by Get when the key is unset. Callers that want
// a fallback should use GetString / GetInt / GetFloat / GetBool instead.
var ErrNotFound = errors.New("settings: not found")

const schemaSQL = `
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`

// Store is a thin SQLite-backed key/value store.
type Store struct {
	db    *sql.DB
	now   func() time.Time
	owned bool
}

// OpenStore opens (or creates) the SQLite file at path and applies the
// settings schema. The caller is responsible for Close.
func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("settings: db path required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("settings: open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("settings: ping db: %w", err)
	}
	s := &Store{db: db, now: time.Now, owned: true}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// NewStoreWithDB shares an existing *sql.DB. Useful when the bot has
// already opened the scheduler/auth DB and wants settings on the same
// connection pool.
func NewStoreWithDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db, now: time.Now, owned: false}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the underlying DB if Store owns it.
func (s *Store) Close() error {
	if s == nil || !s.owned {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("settings: migrate: %w", err)
	}
	return nil
}

// Get returns the raw string value for key. Returns ErrNotFound when no
// row exists.
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	if s == nil {
		return "", ErrNotFound
	}
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("settings: get %s: %w", key, err)
	}
	return v, nil
}

// GetString returns the value for key or fallback when missing / blank.
// A blank stored value is treated as missing so an operator can clear
// a field via the UI without removing the row.
func (s *Store) GetString(ctx context.Context, key, fallback string) string {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// GetInt returns the integer value for key or fallback when missing /
// unparseable. Mirrors getEnvInt's "fail-soft" behavior in env.go so
// settings semantics match env semantics exactly.
func (s *Store) GetInt(ctx context.Context, key string, fallback int) int {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

// GetFloat returns the float value for key or fallback when missing /
// unparseable.
func (s *Store) GetFloat(ctx context.Context, key string, fallback float64) float64 {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return fallback
	}
	return f
}

// GetBool returns the boolean value for key or fallback when missing /
// unparseable.
func (s *Store) GetBool(ctx context.Context, key string, fallback bool) bool {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return b
}

// Set writes value for key, replacing any existing row.
func (s *Store) Set(ctx context.Context, key, value string) error {
	if s == nil {
		return errors.New("settings: nil store")
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("settings: key required")
	}
	now := s.now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, now)
	if err != nil {
		return fmt.Errorf("settings: set %s: %w", key, err)
	}
	return nil
}

// Delete removes the row for key. Idempotent — deleting a missing key
// returns nil.
func (s *Store) Delete(ctx context.Context, key string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("settings: delete %s: %w", key, err)
	}
	return nil
}

// All returns the full key/value map. Used by the dashboard form to
// pre-populate the settings page.
func (s *Store) All(ctx context.Context) (map[string]string, error) {
	if s == nil {
		return map[string]string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("settings: list: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("settings: scan: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}
