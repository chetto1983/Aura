// Package auth manages bearer tokens for the dashboard HTTP API.
//
// Threat model (PDR phase-10-ui §10d):
//   - Tokens are minted by the bot only when the user is in the Telegram
//     allowlist. The Telegram chat is the issuance channel; tokens never
//     traverse an unauthenticated HTTP path.
//   - Only the SHA-256 hash of a token is stored. The plaintext leaves
//     the process exactly once (Issue's return + Telegram send).
//   - Lookup uses crypto/subtle.ConstantTimeCompare for the hash compare,
//     even though SQLite already indexes by hash — keeps the door closed
//     against future code paths that might compare manually.
//   - last_used is updated inline on each Lookup. MVP — if it shows up
//     as a hot row, batch the writes per the design note.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrInvalid is returned by Lookup when the token is unknown, malformed,
// or revoked. The middleware translates it to 401 — the API never
// distinguishes "wrong token" from "revoked token" to a client.
var ErrInvalid = errors.New("auth: invalid token")

// schemaSQL bootstraps the api_tokens table. Idempotent.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS api_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    issued_at  TEXT NOT NULL,
    last_used  TEXT,
    revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
`

// Store wraps a *sql.DB with the SQL needed to mint, look up, and revoke
// API tokens. Callers using OpenStore own the close lifecycle; callers
// using NewStoreWithDB share a connection with another subsystem.
type Store struct {
	db    *sql.DB
	now   func() time.Time
	owned bool
}

// OpenStore opens (or creates) the SQLite file at path and applies the
// auth schema. The caller is responsible for Close.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping auth db: %w", err)
	}
	s := &Store{db: db, now: time.Now, owned: true}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// NewStoreWithDB shares an existing *sql.DB so auth can co-locate with
// another subsystem (typically scheduler) on the same file.
func NewStoreWithDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db, now: time.Now, owned: false}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the underlying DB if Store owns it.
func (s *Store) Close() error {
	if !s.owned {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("auth migrate: %w", err)
	}
	return nil
}

// Issue mints a fresh token for userID, persists its SHA-256 hash, and
// returns the bare token. The plaintext is the only copy the caller will
// ever see — Lookup verifies by hash compare.
func (s *Store) Issue(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", errors.New("auth: user id required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := hashToken(token)
	now := s.now().UTC().Format(time.RFC3339)

	const q = `
		INSERT INTO api_tokens (token_hash, user_id, issued_at, last_used, revoked_at)
		VALUES (?, ?, ?, NULL, NULL)
	`
	if _, err := s.db.ExecContext(ctx, q, hash, userID, now); err != nil {
		return "", fmt.Errorf("auth issue: %w", err)
	}
	return token, nil
}

// Lookup verifies token and returns the associated user ID. Updates the
// row's last_used inline. Returns ErrInvalid for unknown / malformed /
// revoked tokens; never leaks why through the error.
func (s *Store) Lookup(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrInvalid
	}
	hash := hashToken(token)
	const q = `SELECT token_hash, user_id, revoked_at FROM api_tokens WHERE token_hash = ?`
	var (
		gotHash   string
		userID    string
		revokedAt sql.NullString
	)
	row := s.db.QueryRowContext(ctx, q, hash)
	if err := row.Scan(&gotHash, &userID, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInvalid
		}
		return "", fmt.Errorf("auth lookup: %w", err)
	}
	// Belt-and-suspenders: SQLite already keyed on the hash, but the
	// constant-time compare keeps a future code path from regressing
	// into a non-constant-time substring match.
	if subtle.ConstantTimeCompare([]byte(gotHash), []byte(hash)) != 1 {
		return "", ErrInvalid
	}
	if revokedAt.Valid && revokedAt.String != "" {
		return "", ErrInvalid
	}
	// last_used is best-effort; a write failure here doesn't invalidate
	// the lookup. Logging happens at the middleware layer.
	now := s.now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, now, hash)
	return userID, nil
}

// Revoke flips revoked_at on the row whose hash matches token. Returns
// ErrInvalid when no such row exists or it's already revoked. The token
// string itself is never persisted — only the hash.
func (s *Store) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalid
	}
	hash := hashToken(token)
	now := s.now().UTC().Format(time.RFC3339)
	const q = `
		UPDATE api_tokens
		SET revoked_at = ?
		WHERE token_hash = ? AND (revoked_at IS NULL OR revoked_at = '')
	`
	res, err := s.db.ExecContext(ctx, q, now, hash)
	if err != nil {
		return fmt.Errorf("auth revoke: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalid
	}
	return nil
}

// hashToken returns the lowercase hex SHA-256 of token. The hash is the
// only on-disk representation so even a DB leak doesn't yield usable
// bearer credentials.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
