package breakglass

import (
	"context"
	"errors"
	"fmt"

	authulamodels "github.com/Authula/authula/models"
	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/webauth"
)

// NewAuthula builds an OFFLINE Authula provider over dsn (no running server): it
// stands up Authula's own database/sql pool on search_path=authula and runs its bun
// migrations at construction, exposing PasswordService/AccountService/SessionService
// via CoreServices(). secret must be 64 hex chars (webauth.New enforces this). The
// caller owns lifecycle: `defer provider.Close()` stops Authula's expiry workers so
// the process stays goleak-clean under -race. Mirrors scripts/authula_seed_e2e.go.
func NewAuthula(dsn, secret string) (*webauth.Provider, error) {
	return webauth.New(webauth.Config{
		DSN:            dsn,
		Secret:         secret,
		TrustedOrigins: []string{"http://127.0.0.1:9080"},
	})
}

// setOperatorPassword performs the offline break-glass reset against Authula's own
// argon2id services: resolve the operator's authula_user_id, hash the new password
// with core.PasswordService.Hash (the ONLY encoding Argon2PasswordService.Verify
// accepts — never agui.hashArgon2id, whose $aura$… envelope Verify cannot decode),
// delete every session, then persist the account.
//
// Two deviations from the online authulaPasswordResetter.SetPassword it models on:
//   - D-02: NO ErrPasswordResetSamePassword guard — break-glass deliberately allows
//     re-setting the current password (availability over the online same-password
//     rejection).
//   - Ordering is preserved: DeleteAllByUserID runs BEFORE AccountService.Update so a
//     mid-op failure never leaves a live session on a changed password.
//
// A missing auth link or a missing Authula account each returns a clear, secret-free
// error and performs NO write past the failed lookup. The plaintext password is never
// interpolated into any error.
func setOperatorPassword(ctx context.Context, pool *pgxpool.Pool, core *authulaservices.CoreServices, identityID, password string) error {
	if pool == nil || core == nil || core.PasswordService == nil ||
		core.AccountService == nil || core.SessionService == nil {
		return errors.New("break-glass password backend unavailable")
	}
	id, err := uuid.Parse(identityID)
	if err != nil {
		return fmt.Errorf("parse operator identity id: %w", err)
	}

	var authulaUserID string
	if err := pool.QueryRow(ctx,
		`SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id=$1`,
		pgtype.UUID{Bytes: id, Valid: true},
	).Scan(&authulaUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("operator has no authula auth link")
		}
		return fmt.Errorf("resolve operator authula auth link: %w", err)
	}

	account, err := core.AccountService.GetByUserIDAndProvider(ctx, authulaUserID, authulamodels.AuthProviderEmail.String())
	if err != nil {
		return fmt.Errorf("load operator authula account: %w", err)
	}
	if account == nil {
		return errors.New("operator has no authula email account")
	}

	// D-02: intentionally NO same-password (ErrPasswordResetSamePassword) comparison.
	hash, err := core.PasswordService.Hash(password)
	if err != nil {
		return errors.New("hash operator password: authula password service failed")
	}
	account.Password = &hash

	// Delete-then-update: kill every session BEFORE persisting the new hash.
	if err := core.SessionService.DeleteAllByUserID(ctx, authulaUserID); err != nil {
		return fmt.Errorf("delete operator sessions: %w", err)
	}
	if _, err := core.AccountService.Update(ctx, account); err != nil {
		return fmt.Errorf("update operator account: %w", err)
	}
	return nil
}
