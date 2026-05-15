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
	"strings"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
)

// ErrInvalid is returned by Lookup when the token is unknown, malformed,
// or revoked. The middleware translates it to 401 — the API never
// distinguishes "wrong token" from "revoked token" to a client.
var ErrInvalid = errors.New("auth: invalid token")

// ErrExpired is returned by Lookup when a known token is past expires_at.
var ErrExpired = errors.New("auth: token expired")

// AuditUpdateError reports that token lookup succeeded, but updating the
// best-effort last_used audit field failed.
type AuditUpdateError struct {
	UserID string
	Err    error
}

func (e *AuditUpdateError) Error() string {
	return "auth: token audit update failed"
}

func (e *AuditUpdateError) Unwrap() error {
	return e.Err
}

const defaultTokenTTL = 30 * 24 * time.Hour

const (
	// SourceTelegramBootstrap is the normal first-run owner claim path.
	SourceTelegramBootstrap = "telegram_bootstrap"
	// SourceTelegramEnvAllowlist identifies users trusted by TELEGRAM_ALLOWLIST.
	// It creates identity/grants without copying config into allowed_users.
	SourceTelegramEnvAllowlist = "telegram_env_allowlist"
	// SourceDashboardApprove is the dashboard-owner approval path.
	SourceDashboardApprove = "dashboard_approve"
	// SourceE2EBootstrap grants dashboard access for local smoke tests
	// without counting as a real owner that can block first-run setup.
	SourceE2EBootstrap = "e2e_bootstrap"
)

// TokenReader is the read side needed by dashboard bearer middleware.
type TokenReader interface {
	Lookup(ctx context.Context, token string) (string, error)
}

// TokenIssuer is the write side needed by token minting flows.
type TokenIssuer interface {
	Issue(ctx context.Context, userID string) (string, error)
}

// TokenRevoker is the write side needed by logout and failed-delivery flows.
type TokenRevoker interface {
	Revoke(ctx context.Context, token string) error
}

// TokenWriter is the complete token mutation side.
type TokenWriter interface {
	TokenIssuer
	TokenRevoker
}

// TokenRepository is the complete dashboard token persistence boundary.
type TokenRepository interface {
	TokenReader
	TokenWriter
}

// AccessReader is the read side of persisted Telegram/dashboard allowlists.
type AccessReader interface {
	IsUserAllowed(ctx context.Context, userID string) (bool, error)
	AllowedUserCount(ctx context.Context) (int, error)
	AllowedUserIDs(ctx context.Context) ([]string, error)
}

// AccessWriter is the write side of bootstrap and pending-approval flows.
type AccessWriter interface {
	BootstrapUser(ctx context.Context, userID string) (bool, error)
	BootstrapE2EUser(ctx context.Context, userID string) (bool, error)
	EnsureTelegramAllowlistedIdentity(ctx context.Context, userID, source string) (identity.TelegramAllowlistBackfillResult, error)
	RequestAccess(ctx context.Context, userID, username string) (bool, error)
	Approve(ctx context.Context, userID string) error
	Deny(ctx context.Context, userID string) error
}

// PendingReader is the dashboard read side for open approval requests.
type PendingReader interface {
	ListPending(ctx context.Context) ([]PendingUser, error)
}

// DashboardRepository is the API surface needed by bearer auth, logout, and
// pending request listing. Approval mutation is handled by PendingApprover in
// internal/api so plaintext dashboard tokens still travel through Telegram.
type DashboardRepository interface {
	TokenReader
	TokenRevoker
	PendingReader
	Authorizer
}

// Authorizer is the dashboard authorization read side.
type Authorizer interface {
	Authorize(ctx context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error)
}

// Repository is the full auth persistence boundary used by Telegram wiring.
type Repository interface {
	TokenRepository
	AccessReader
	AccessWriter
	PendingReader
	Authorizer
}

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
		db.Close()
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
			db.Close()
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

// Authorize delegates dashboard authorization checks to the identity store.
func (s *Store) Authorize(ctx context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return s.identity.Authorize(ctx, params)
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
		WHERE NOT EXISTS (SELECT 1 FROM allowed_users WHERE source <> ?)
	`, userID, SourceTelegramBootstrap, now, SourceE2EBootstrap)
	if err != nil {
		return false, fmt.Errorf("auth bootstrap: insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("auth bootstrap: rows affected: %w", err)
	}
	if n > 0 {
		if err := s.closePendingAsApproved(ctx, userID, now); err != nil {
			return false, err
		}
		if err := s.backfillAllowedUserIdentity(ctx, userID, SourceTelegramBootstrap); err != nil {
			return false, err
		}
		return true, nil
	}
	allowed, err := s.IsUserAllowed(ctx, userID)
	if err != nil || !allowed {
		return allowed, err
	}
	return allowed, s.backfillAllowedUserIdentityFromRow(ctx, userID)
}

// BootstrapE2EUser grants dashboard access for local browser smoke tests
// only while no real owner exists. E2E users are intentionally ignored by
// BootstrapUser's first-owner check so a test seed can never strand the
// operator in the pending approval loop.
func (s *Store) BootstrapE2EUser(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, errors.New("auth: user id required")
	}
	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO allowed_users (user_id, source, created_at)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM allowed_users WHERE source <> ?)
		ON CONFLICT(user_id) DO NOTHING
	`, userID, SourceE2EBootstrap, now, SourceE2EBootstrap)
	if err != nil {
		return false, fmt.Errorf("auth e2e bootstrap: insert: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("auth e2e bootstrap: rows affected: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	return s.IsUserAllowed(ctx, userID)
}

func (s *Store) closePendingAsApproved(ctx context.Context, userID, decidedAt string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE pending_users
		SET decided_at = ?, decision = 'approved'
		WHERE user_id = ? AND decision IS NULL
	`, decidedAt, userID); err != nil {
		return fmt.Errorf("auth bootstrap: close pending: %w", err)
	}
	return nil
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

// AllowedUserIDs returns the set of real currently-allowlisted Telegram user
// IDs persisted by BootstrapUser/Approve. E2E bootstrap rows are intentionally
// excluded so scheduled jobs do not try to notify synthetic dashboard users.
func (s *Store) AllowedUserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM allowed_users WHERE source <> ? ORDER BY created_at`, SourceE2EBootstrap)
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
		VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO NOTHING
	`, userID, SourceDashboardApprove, now); err != nil {
		return fmt.Errorf("auth approve: insert allowed: %w", err)
	}
	identityStore, err := identity.NewStoreWithTx(tx, identity.WithNow(func() time.Time { return s.now() }))
	if err != nil {
		return fmt.Errorf("auth approve: identity tx: %w", err)
	}
	if err := s.backfillAllowedUserIdentityWithStore(ctx, identityStore, userID, SourceDashboardApprove); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("auth approve: commit: %w", err)
	}
	return nil
}

func (s *Store) BackfillAllowedUserIdentities(ctx context.Context) (identity.TelegramAllowlistBackfillSummary, error) {
	var summary identity.TelegramAllowlistBackfillSummary
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, source
		FROM allowed_users
		WHERE source <> ?
		ORDER BY created_at
	`, SourceE2EBootstrap)
	if err != nil {
		return summary, fmt.Errorf("auth identity backfill: list allowed users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID, source string
		if err := rows.Scan(&userID, &source); err != nil {
			return summary, fmt.Errorf("auth identity backfill: scan allowed user: %w", err)
		}
		result, err := s.backfillAllowedUserIdentityResult(ctx, userID, source)
		if err != nil {
			return summary, err
		}
		summary.Users++
		if result.PrincipalCreated {
			summary.PrincipalsCreated++
		}
		if result.ChannelAccountCreated {
			summary.ChannelAccountsCreated++
		}
		if result.ActorCreated {
			summary.ActorsCreated++
		}
		summary.GrantsCreated += result.GrantsCreated
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("auth identity backfill: iterate allowed users: %w", err)
	}
	return summary, nil
}

// EnsureTelegramAllowlistedIdentity creates the deterministic Telegram
// principal/channel-account/session-actor/grants needed before token issuance.
// When source is blank, the source is derived from the persisted allowed_users
// row. Env allowlist callers pass SourceTelegramEnvAllowlist so config remains
// config and does not become an allowed_users row.
func (s *Store) EnsureTelegramAllowlistedIdentity(ctx context.Context, userID, source string) (identity.TelegramAllowlistBackfillResult, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return identity.TelegramAllowlistBackfillResult{}, errors.New("auth: user id required")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		var err error
		source, err = s.allowedUserSource(ctx, userID)
		if err != nil {
			return identity.TelegramAllowlistBackfillResult{}, err
		}
	}
	return s.backfillAllowedUserIdentityResult(ctx, userID, source)
}

func (s *Store) backfillAllowedUserIdentityFromRow(ctx context.Context, userID string) error {
	source, err := s.allowedUserSource(ctx, userID)
	if err != nil {
		return err
	}
	return s.backfillAllowedUserIdentity(ctx, userID, source)
}

func (s *Store) backfillAllowedUserIdentity(ctx context.Context, userID, source string) error {
	_, err := s.backfillAllowedUserIdentityResult(ctx, userID, source)
	return err
}

func (s *Store) backfillAllowedUserIdentityResult(ctx context.Context, userID, source string) (identity.TelegramAllowlistBackfillResult, error) {
	return s.backfillAllowedUserIdentityWithStoreResult(ctx, s.identity, userID, source)
}

func (s *Store) backfillAllowedUserIdentityWithStore(ctx context.Context, identityStore *identity.Store, userID, source string) error {
	_, err := s.backfillAllowedUserIdentityWithStoreResult(ctx, identityStore, userID, source)
	return err
}

func (s *Store) backfillAllowedUserIdentityWithStoreResult(ctx context.Context, identityStore *identity.Store, userID, source string) (identity.TelegramAllowlistBackfillResult, error) {
	if identityStore == nil || strings.TrimSpace(source) == SourceE2EBootstrap {
		return identity.TelegramAllowlistBackfillResult{}, nil
	}
	result, err := identityStore.BackfillTelegramAllowlistedUser(ctx, identity.TelegramAllowlistUserParams{
		UserID:        userID,
		Source:        source,
		PrincipalKind: principalKindForAllowedUserSource(source),
		Capabilities:  capabilitiesForAllowedUserSource(source),
	})
	if err != nil {
		return identity.TelegramAllowlistBackfillResult{}, fmt.Errorf("auth identity backfill: %w", err)
	}
	return result, nil
}

func (s *Store) allowedUserSource(ctx context.Context, userID string) (string, error) {
	var source string
	if err := s.db.QueryRowContext(ctx, `SELECT source FROM allowed_users WHERE user_id = ?`, userID).Scan(&source); err != nil {
		return "", fmt.Errorf("auth identity backfill: allowed user source: %w", err)
	}
	return source, nil
}

func principalKindForAllowedUserSource(source string) identity.PrincipalKind {
	switch strings.TrimSpace(source) {
	case SourceTelegramBootstrap, SourceTelegramEnvAllowlist, "manual":
		return identity.PrincipalKindOwner
	default:
		return identity.PrincipalKindHuman
	}
}

func capabilitiesForAllowedUserSource(source string) []identity.Capability {
	if principalKindForAllowedUserSource(source) == identity.PrincipalKindOwner {
		return identity.TelegramOwnerCapabilities()
	}
	return identity.TelegramUserCapabilities()
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
