// Break-glass recovery for `aura identity recover <name>` (MUSR-06, D-16). This
// is the HOST-ONLY credential-reset path: the operator on the host is trusted
// (host access is the ownership proof), so no running server and no standing
// server route mints an admin bypass. It reuses the already-shipped short-lived
// hashed-token infra (aura.password_reset_challenges + aura.password_reset_tokens,
// migration 0023) exactly — no new token scheme. The one-time plaintext token is
// printed to stdout for the operator to hand to the user and is NEVER logged.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
)

// breakGlassTokenTTL is the short-lived lifetime of a host-minted break-glass
// reset token. It reuses the online self-service reset-token lifetime exactly
// (serve_password_reset.go mints 10-minute tokens): host access is the ownership
// proof (D-16), so the bearer window stays deliberately short.
const breakGlassTokenTTL = 10 * time.Minute

// breakGlassAuditEvent is the neutral, append-only audit event recorded when a
// break-glass token is minted. It carries no plaintext token, code, or secret.
const breakGlassAuditEvent = "break_glass_token_minted"

// identityRecover implements `aura identity recover <name>`. It resolves the
// identity by name and mints a short-lived hashed reset token, printing the
// one-time plaintext token to stdout. An unknown name exits non-zero.
func identityRecover(ctx context.Context, store *identity.Store, pool *pgxpool.Pool, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, identityUsage)
		os.Exit(1)
	}
	name := args[0]
	id, err := store.GetIdentityByName(ctx, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	token, err := mintBreakGlassToken(ctx, pool, id.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// stdout only — the operator hands this one-time token to the user. NEVER slog.
	fmt.Printf("ok: minted break-glass reset token for %s (expires in %s)\n", name, breakGlassTokenTTL)
	fmt.Println(token)
}

// mintBreakGlassToken mints a short-lived hashed reset token for identityID by
// reusing the 0023 challenge+token infra exactly (the serve_password_reset.go
// shape): it creates the parent challenge row the token FK requires, consumes it,
// inserts the hashed token, and records a neutral audit event — all atomically.
// Returns the one-time PLAINTEXT token; only its sha256 hash is persisted.
func mintBreakGlassToken(ctx context.Context, pool *pgxpool.Pool, identityID string) (string, error) {
	uid, err := parsePasswordResetIdentityID(identityID)
	if err != nil {
		return "", err
	}
	pgID := pgtype.UUID{Bytes: uid, Valid: true}

	// The plaintext token handed to the operator; only HashLookupToken(token) is stored.
	token, err := newPasswordResetToken()
	if err != nil {
		return "", err
	}
	// A throwaway secret satisfies the challenge's NOT NULL code_hash. It is never
	// revealed and the challenge is consumed in the same tx, so the online
	// self-service flow can never complete this challenge.
	challengeSecret, err := newPasswordResetToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	tq := sqlc.New(tx)

	challenge, err := tq.InsertPasswordResetChallenge(ctx, breakGlassChallengeParams(pgID, agui.HashLookupToken(challengeSecret), now))
	if err != nil {
		return "", err
	}
	consumed, err := tq.ConsumePasswordResetChallenge(ctx, challenge.ID)
	if err != nil {
		return "", err
	}
	if _, err := tq.InsertPasswordResetToken(ctx, breakGlassTokenParams(agui.HashLookupToken(token), consumed.ID, consumed.IdentityID, now)); err != nil {
		return "", err
	}
	if _, err := tq.InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
		IdentityID: consumed.IdentityID,
		Event:      breakGlassAuditEvent,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	committed = true
	return token, nil
}

// breakGlassChallengeParams builds the parent challenge insert params. The
// challenge exists only to satisfy the token FK; telegram_user_id is absent
// (host-minted, not Telegram-delivered) and MaxAttempts reuses the shipped
// challenge cap.
func breakGlassChallengeParams(identityID pgtype.UUID, codeHash string, now time.Time) sqlc.InsertPasswordResetChallengeParams {
	return sqlc.InsertPasswordResetChallengeParams{
		IdentityID:  identityID,
		CodeHash:    codeHash,
		ExpiresAt:   pgtype.Timestamptz{Time: now.Add(breakGlassTokenTTL), Valid: true},
		MaxAttempts: passwordResetChallengeMaxAttempts,
	}
}

// breakGlassTokenParams builds the reset-token insert params, reusing the exact
// expires_at + max_attempts shape from serve_password_reset.go's online mint.
func breakGlassTokenParams(tokenHash string, challengeID, identityID pgtype.UUID, now time.Time) sqlc.InsertPasswordResetTokenParams {
	return sqlc.InsertPasswordResetTokenParams{
		TokenHash:   tokenHash,
		ChallengeID: challengeID,
		IdentityID:  identityID,
		ExpiresAt:   pgtype.Timestamptz{Time: now.Add(breakGlassTokenTTL), Valid: true},
		MaxAttempts: passwordResetTokenMaxAttempts,
	}
}
