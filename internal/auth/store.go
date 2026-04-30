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

CREATE TABLE IF NOT EXISTS allowed_users (
    user_id    TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pending_users (
    user_id      TEXT PRIMARY KEY,
    username     TEXT NOT NULL,
    requested_at TEXT NOT NULL,
    decided_at   TEXT,
    decision     TEXT
);
CREATE INDEX IF NOT EXISTS idx_pending_users_decision ON pending_users(decision);
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

// BootstrapUser claims the first-run allowlist for userID. It inserts the
// user only when no persisted bootstrap user exists yet.
func (s *Store) BootstrapUser(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, errors.New("auth: user id required")
	}
	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO allowed_users (user_id, source, created_at)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM allowed_users)
	`, userID, "telegram_bootstrap", now)
	if err != nil {
		return false, fmt.Errorf("auth bootstrap: insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("auth bootstrap: rows affected: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	return s.IsUserAllowed(ctx, userID)
}

// IsUserAllowed reports whether userID has been persisted by BootstrapUser.
func (s *Store) IsUserAllowed(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allowed_users WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return false, fmt.Errorf("auth allowed user lookup: %w", err)
	}
	return count > 0, nil
}

// AllowedUserCount returns the number of persisted bootstrap users.
func (s *Store) AllowedUserCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allowed_users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("auth allowed user count: %w", err)
	}
	return count, nil
}

// AllowedUserIDs returns the set of currently-allowlisted Telegram user
// IDs persisted by BootstrapUser/Approve. Used by the bot to fan out
// pending-request notifications. The slice is empty when no env-allowlist
// exists yet AND no user has been bootstrapped.
func (s *Store) AllowedUserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM allowed_users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("auth allowed user list: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("auth allowed user scan: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PendingUser is one row of the pending_users table.
type PendingUser struct {
	UserID      string
	Username    string
	RequestedAt time.Time
}

// RequestAccess records a pending access request for userID. Idempotent —
// a second call from the same user just refreshes username / requested_at
// so the dashboard always shows the most recent intent. Returns true when
// this is a freshly-created request (caller can use that to gate the
// notification fan-out so a user spamming /start doesn't ping the owner
// every time).
func (s *Store) RequestAccess(ctx context.Context, userID, username string) (bool, error) {
	if userID == "" {
		return false, errors.New("auth: user id required")
	}
	now := s.now().UTC().Format(time.RFC3339)
	// Insert if missing; treat that as the "fresh" signal. If the row
	// exists and is still pending (decision IS NULL), do not bump
	// requested_at — keeps the dashboard ordering stable and prevents
	// notification spam from re-/start. If the row exists with a prior
	// decision, reset it back to pending and treat as fresh.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO pending_users (user_id, username, requested_at, decided_at, decision)
		VALUES (?, ?, ?, NULL, NULL)
		ON CONFLICT(user_id) DO UPDATE SET
		    username = excluded.username,
		    requested_at = CASE WHEN pending_users.decision IS NULL THEN pending_users.requested_at ELSE excluded.requested_at END,
		    decided_at = NULL,
		    decision = NULL
		WHERE pending_users.decision IS NOT NULL
	`, userID, username, now)
	if err != nil {
		return false, fmt.Errorf("auth request access: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("auth request access: rows affected: %w", err)
	}
	// rows-affected is 1 on fresh insert OR on the UPDATE branch (decision
	// was non-null). 0 means "row already pending" — not fresh.
	return n > 0, nil
}

// ListPending returns the open access requests (decision IS NULL) ordered
// oldest first so the dashboard shows them in arrival order.
func (s *Store) ListPending(ctx context.Context) ([]PendingUser, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, username, requested_at
		FROM pending_users
		WHERE decision IS NULL
		ORDER BY requested_at
	`)
	if err != nil {
		return nil, fmt.Errorf("auth list pending: %w", err)
	}
	defer rows.Close()
	var out []PendingUser
	for rows.Next() {
		var p PendingUser
		var ts string
		if err := rows.Scan(&p.UserID, &p.Username, &ts); err != nil {
			return nil, fmt.Errorf("auth pending scan: %w", err)
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return nil, fmt.Errorf("auth pending parse time: %w", err)
		}
		p.RequestedAt = t
		out = append(out, p)
	}
	return out, rows.Err()
}

// Approve moves a pending request into allowed_users atomically. Returns
// ErrInvalid when no open pending row exists for userID. The caller is
// responsible for sending the freshly-onboarded user a dashboard token.
func (s *Store) Approve(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth approve: begin: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_users
		SET decided_at = ?, decision = 'approved'
		WHERE user_id = ? AND decision IS NULL
	`, now, userID)
	if err != nil {
		return fmt.Errorf("auth approve: update pending: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth approve: rows affected: %w", err)
	}
	if n == 0 {
		return ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO allowed_users (user_id, source, created_at)
		VALUES (?, 'dashboard_approve', ?)
		ON CONFLICT(user_id) DO NOTHING
	`, userID, now); err != nil {
		return fmt.Errorf("auth approve: insert allowed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("auth approve: commit: %w", err)
	}
	return nil
}

// Deny rejects a pending request without granting access. Returns
// ErrInvalid when no open pending row exists for userID. The row is kept
// (not deleted) so a future /start refreshes the request rather than
// silently re-queueing — that keeps an audit trail and surfaces repeat
// attempts to the dashboard owner.
func (s *Store) Deny(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalid
	}
	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_users
		SET decided_at = ?, decision = 'denied'
		WHERE user_id = ? AND decision IS NULL
	`, now, userID)
	if err != nil {
		return fmt.Errorf("auth deny: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth deny: rows affected: %w", err)
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
