// Package telegram is the Telegram channel (Phase 13 / Slice 9a, UX-02). This file
// is the DB seam: a Store over aura.telegram_accounts + aura.telegram_setup_pending
// that copies the canonical Store pattern proved in internal/identity and reused by
// internal/askuser (D-A4-01): Store{pool,q} over the generated sqlc surface,
// SQLSTATE-based error classification via errors.As + pgErr.Code (never message
// matching), sentinel errors, pgtype conversion at the boundary, and db.WithTx for
// the atomic consume-pending-then-INSERT-account write.
//
// Both onboarding (UX-03, the setup wizard mints pending tokens) and the channel
// itself (UX-02, /start consumes a token and persists the account) sit on this
// Store. ConsumeOnboarding is the single-use credential chokepoint
// (T-13-01-TokenReplay): one db.WithTx marks consumed_at and INSERTs the account;
// a re-consume of a spent/expired token returns ErrTokenConsumed and writes no
// duplicate account.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors so callers classify failures without string matching.
// ErrTokenConsumed is an already-consumed OR expired onboarding token (a spent
// single-use credential); ErrTokenNotFound is an unknown token; ErrAccountExists
// is a duplicate telegram_user_id (SQLSTATE 23505 on the account INSERT).
var (
	ErrTokenConsumed = errors.New("onboarding token already consumed or expired")
	ErrTokenNotFound = errors.New("onboarding token not found")
	ErrAccountExists = errors.New("telegram account already exists")
)

// Store wraps a pgx pool and the generated Queries — the canonical shape. Non-tx
// reads/writes use s.q; the atomic consume-and-INSERT wraps db.WithTx.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// Account is the domain projection of an aura.telegram_accounts row — plain Go
// types at the package boundary instead of the sqlc pgtype wrappers.
type Account struct {
	TelegramUserID int64
	IdentityID     string
	Username       string
	FirstName      string
	AddedAt        time.Time
	LastSeenAt     time.Time // zero when never seen (last_seen_at SQL NULL)
}

// InsertPendingParams carries the plain fields for one new onboarding token.
type InsertPendingParams struct {
	OnboardingToken string
	IdentityID      string
	GeneratedBy     string    // optional source tag (empty → SQL NULL)
	ExpiresAt       time.Time // created_at + 1h at the call site
}

// ConsumeParams carries the account fields written when an onboarding token is
// consumed. IdentityID is taken from the consumed pending row, not from here, so
// the account always FKs the identity the token was minted for.
type ConsumeParams struct {
	OnboardingToken string
	TelegramUserID  int64
	Username        string // optional (empty → SQL NULL)
	FirstName       string // optional (empty → SQL NULL)
}

// InsertPending persists one pending onboarding token (UX-03 mint side).
func (s *Store) InsertPending(ctx context.Context, p InsertPendingParams) error {
	idID, err := db.ParseUUID("identity_id", p.IdentityID)
	if err != nil {
		return fmt.Errorf("insert telegram setup pending: %w", err)
	}
	arg := sqlc.InsertTelegramSetupPendingParams{
		OnboardingToken: p.OnboardingToken,
		IdentityID:      idID,
		GeneratedBy:     textOrNull(p.GeneratedBy),
		ExpiresAt:       pgtype.Timestamptz{Time: p.ExpiresAt, Valid: true},
	}
	if err := s.q.InsertTelegramSetupPending(ctx, arg); err != nil {
		return fmt.Errorf("insert telegram setup pending %s: %w", p.OnboardingToken, err)
	}
	return nil
}

// ConsumeOnboarding is the single-use credential chokepoint (T-13-01-TokenReplay):
// in ONE db.WithTx it (1) atomically marks the token consumed (the SQL guards
// consumed_at IS NULL AND expires_at > now() make the UPDATE match no row for a
// spent/expired token → ErrTokenConsumed) and (2) INSERTs the telegram_accounts
// row FK'd to the identity the token was minted for. A duplicate account (the same
// telegram_user_id already onboarded) classifies via SQLSTATE 23505 →
// ErrAccountExists, never a message match. Any error rolls the whole tx back, so a
// failed account INSERT leaves the token unconsumed (no half-onboarded state).
func (s *Store) ConsumeOnboarding(ctx context.Context, p ConsumeParams) (Account, error) {
	var acct Account
	err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
		pending, err := q.ConsumeTelegramSetupPending(ctx, p.OnboardingToken)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// No row matched: the token is unknown, already consumed, or expired.
				// Single-use enforcement collapses all three to ErrTokenConsumed for
				// a consume attempt (GetPending distinguishes unknown if a caller needs it).
				return fmt.Errorf("consume onboarding %s: %w", p.OnboardingToken, ErrTokenConsumed)
			}
			return fmt.Errorf("consume onboarding %s: %w", p.OnboardingToken, err)
		}
		row, err := q.InsertTelegramAccount(ctx, sqlc.InsertTelegramAccountParams{
			TelegramUserID: p.TelegramUserID,
			IdentityID:     pending.IdentityID,
			Username:       textOrNull(p.Username),
			FirstName:      textOrNull(p.FirstName),
		})
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("consume onboarding %s: %w", p.OnboardingToken, ErrAccountExists)
			}
			return fmt.Errorf("consume onboarding %s: insert account: %w", p.OnboardingToken, err)
		}
		acct = accountFromRow(row)
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return acct, nil
}

// CleanupExpired deletes unconsumed onboarding tokens past their expiry and returns
// the number removed (the setup SSE-pump GC scan).
func (s *Store) CleanupExpired(ctx context.Context) (int64, error) {
	n, err := s.q.DeleteExpiredTelegramSetupPending(ctx)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired telegram setup pending: %w", err)
	}
	return n, nil
}

// PendingConsumed reports whether a token row exists and has been consumed, mapping
// a missing row to ErrTokenNotFound. Used by the setup-status / SSE-poll path.
func (s *Store) PendingConsumed(ctx context.Context, onboardingToken string) (bool, error) {
	row, err := s.q.GetTelegramSetupPending(ctx, onboardingToken)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("get telegram setup pending %s: %w", onboardingToken, ErrTokenNotFound)
		}
		return false, fmt.Errorf("get telegram setup pending %s: %w", onboardingToken, err)
	}
	return row.ConsumedAt.Valid, nil
}

// GetAccountByTelegramID fetches one account by telegram_user_id, mapping a missing
// row to ErrTokenNotFound's sibling — here a not-found account is surfaced via
// pgx.ErrNoRows wrapped so callers can errors.Is it.
func (s *Store) GetAccountByTelegramID(ctx context.Context, telegramUserID int64) (Account, error) {
	row, err := s.q.GetTelegramAccountByTelegramID(ctx, telegramUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, fmt.Errorf("get telegram account %d: %w", telegramUserID, pgx.ErrNoRows)
		}
		return Account{}, fmt.Errorf("get telegram account %d: %w", telegramUserID, err)
	}
	return accountFromRow(row), nil
}

// GetAccountByIdentity fetches one account by its owning identity_id, the key the
// scheduler's identity-routed delivery uses (Phase 20 R3). It mirrors
// GetAccountByTelegramID's error classification but adds the parseUUID boundary the
// generated query needs (identity_id is uuid). A non-UUID identity (e.g. the CLI's
// 'local') can never match a real account, so a parse failure maps to a wrapped
// pgx.ErrNoRows — the same not-found signal as a missing row. That lets Deliver
// use a single errors.Is(err, pgx.ErrNoRows) branch to mean "not my user" for both
// the no-account case AND the 'local' case (Pitfall 6), never surfacing an error.
func (s *Store) GetAccountByIdentity(ctx context.Context, identityID string) (Account, error) {
	idID, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return Account{}, fmt.Errorf("get telegram account by identity %q: %w", identityID, pgx.ErrNoRows)
	}
	row, err := s.q.GetTelegramAccountByIdentity(ctx, idID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, fmt.Errorf("get telegram account by identity %q: %w", identityID, pgx.ErrNoRows)
		}
		return Account{}, fmt.Errorf("get telegram account by identity %q: %w", identityID, err)
	}
	return accountFromRow(row), nil
}

// TouchLastSeen bumps last_seen_at for an account (best-effort activity marker).
func (s *Store) TouchLastSeen(ctx context.Context, telegramUserID int64) error {
	if err := s.q.TouchTelegramLastSeen(ctx, telegramUserID); err != nil {
		return fmt.Errorf("touch telegram last seen %d: %w", telegramUserID, err)
	}
	return nil
}

// CountAccounts returns the number of onboarded accounts (setup-status footer).
func (s *Store) CountAccounts(ctx context.Context) (int64, error) {
	n, err := s.q.CountTelegramAccounts(ctx)
	if err != nil {
		return 0, fmt.Errorf("count telegram accounts: %w", err)
	}
	return n, nil
}

// ListAccounts returns all onboarded accounts, oldest first.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.q.ListTelegramAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list telegram accounts: %w", err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFromRow(r))
	}
	return out, nil
}

// accountFromRow projects a generated row onto the domain Account type, converting
// pgtype wrappers to plain Go values at the boundary (invalid → zero value).
func accountFromRow(r sqlc.AuraTelegramAccounts) Account {
	return Account{
		TelegramUserID: r.TelegramUserID,
		IdentityID:     uuid.UUID(r.IdentityID.Bytes).String(),
		Username:       r.Username.String,
		FirstName:      r.FirstName.String,
		AddedAt:        r.AddedAt.Time,
		LastSeenAt:     r.LastSeenAt.Time,
	}
}

// textOrNull maps an empty string to a SQL NULL pgtype.Text (mirrors the askuser
// boundary discipline).
func textOrNull(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// isUniqueViolation classifies a pgx error as SQLSTATE 23505 via errors.As +
// pgErr.Code — never string-matching the message (mirrors internal/identity).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
