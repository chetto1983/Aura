package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aura/aura/internal/identity"
)

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
	defer func() { _ = rows.Close() }()
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

// RequestAccess records a pending access request for userID. Idempotent —
// a second call from the same user just refreshes username / requested_at
// so the dashboard always shows the most recent intent. Returns true when
// this is a freshly-created request.
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
	defer func() { _ = rows.Close() }()
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
// ErrInvalid when no open pending row exists for userID.
func (s *Store) Approve(ctx context.Context, userID string) error {
	if userID == "" {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth approve: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
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
