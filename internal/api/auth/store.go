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
	"errors"
	"time"

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

const (
	// SourceTelegramBootstrap is the normal first-run owner claim path.
	SourceTelegramBootstrap = "telegram_bootstrap"
	// SourceTelegramConfiguredAllowlist identifies users trusted by the
	// configured allowlist. The allowlist may be DB-backed by settings; this
	// creates identity/grants without copying config into allowed_users.
	SourceTelegramConfiguredAllowlist = "telegram_config_allowlist"
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
	identity.Delegator
}

// PendingUser is one row of the pending_users table.
type PendingUser struct {
	UserID      string
	Username    string
	RequestedAt time.Time
}
