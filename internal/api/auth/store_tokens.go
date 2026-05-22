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

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
)

const defaultTokenTTL = 30 * 24 * time.Hour

// Store wraps a *sql.DB with the SQL needed to mint, look up, and revoke
// API tokens. Callers using OpenStore own the close lifecycle; callers
// using NewStoreWithDB share a connection with another subsystem.
type Store struct {
	db             *sql.DB
	identity       *identity.Store
	now            func() time.Time
	tokenTTL       time.Duration
	owned          bool
	updateLastUsed func(context.Context, string, string) error
}

// OpenStore opens (or creates) the SQLite file at path and applies the
// auth schema. The caller is responsible for Close.
func OpenStore(path string) (*Store, error) {
	db, err := auradb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}
	if err := migrations.Run(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth migrate: %w", err)
	}
	return newStoreWithDB(db, true)
}

// NewStoreWithDB shares an existing *sql.DB so auth can co-locate with
// another subsystem (typically scheduler) on the same file.
func NewStoreWithDB(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("auth: db required")
	}
	return newStoreWithDB(db, false)
}

func newStoreWithDB(db *sql.DB, owned bool) (*Store, error) {
	s := &Store{
		db:             db,
		now:            time.Now,
		tokenTTL:       defaultTokenTTL,
		owned:          owned,
		updateLastUsed: defaultUpdateLastUsed(db),
	}
	identityStore, err := identity.NewStore(db, identity.WithNow(func() time.Time { return s.now() }))
	if err != nil {
		if owned {
			_ = db.Close()
		}
		return nil, err
	}
	s.identity = identityStore
	return s, nil
}

// Close closes the underlying DB if Store owns it.
func (s *Store) Close() error {
	if !s.owned {
		return nil
	}
	return s.db.Close()
}

// SetTokenTTL configures how long newly-issued dashboard bearer tokens
// remain valid. Non-positive values restore the default 30-day TTL.
func (s *Store) SetTokenTTL(ttl time.Duration) {
	if s == nil {
		return
	}
	if ttl <= 0 {
		s.tokenTTL = defaultTokenTTL
		return
	}
	s.tokenTTL = ttl
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
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339)
	expiresAt := nowTime.Add(s.tokenTTL).UTC().Format(time.RFC3339)

	const q = `
		INSERT INTO api_tokens (token_hash, user_id, issued_at, expires_at, last_used, revoked_at)
		VALUES (?, ?, ?, ?, NULL, NULL)
	`
	if _, err := s.db.ExecContext(ctx, q, hash, userID, now, expiresAt); err != nil {
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
	const q = `SELECT token_hash, user_id, revoked_at, expires_at FROM api_tokens WHERE token_hash = ?`
	var (
		gotHash   string
		userID    string
		revokedAt sql.NullString
		expiresAt sql.NullString
	)
	row := s.db.QueryRowContext(ctx, q, hash)
	if err := row.Scan(&gotHash, &userID, &revokedAt, &expiresAt); err != nil {
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
	if !expiresAt.Valid || expiresAt.String == "" {
		return "", ErrExpired
	}
	expiry, err := time.Parse(time.RFC3339, expiresAt.String)
	if err != nil {
		return "", fmt.Errorf("auth lookup: parse expires_at: %w", err)
	}
	if !s.now().UTC().Before(expiry) {
		return "", ErrExpired
	}
	// last_used is best-effort; a write failure here doesn't invalidate
	// the lookup. Logging happens at the middleware layer.
	now := s.now().UTC().Format(time.RFC3339)
	if s.updateLastUsed == nil {
		s.updateLastUsed = defaultUpdateLastUsed(s.db)
	}
	if err := s.updateLastUsed(ctx, now, hash); err != nil {
		return userID, &AuditUpdateError{UserID: userID, Err: err}
	}
	return userID, nil
}

func defaultUpdateLastUsed(db *sql.DB) func(context.Context, string, string) error {
	return func(ctx context.Context, now, hash string) error {
		_, err := db.ExecContext(ctx, `UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, now, hash)
		return err
	}
}

// Revoke flips revoked_at on the row whose hash matches token. Returns
// ErrInvalid when no such row exists or it's already revoked.
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

// Authorize delegates dashboard authorization checks to the identity store.
func (s *Store) Authorize(ctx context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return s.identity.Authorize(ctx, params)
}

func (s *Store) DelegateActor(ctx context.Context, params identity.DelegateActorParams) (identity.DelegateActorResult, error) {
	return s.identity.DelegateActor(ctx, params)
}

// hashToken returns the lowercase hex SHA-256 of token. The hash is the
// only on-disk representation so even a DB leak doesn't yield usable
// bearer credentials.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
