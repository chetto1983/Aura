//go:build db_integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// The password reset and the recovery find the Telegram chat before anyone is signed in, so
// both read the links with no identity bound: the links' row security keeps that read open.
func TestPasswordResetFindsTheTelegramLinkWithNoIdentity(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := t.Context()
	id := uuid.New()
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	email := "reset-" + id.String()[:8] + "@example.test"
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, pgID, email); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, pgID) })

	const telegramUserID = int64(9_000_050_001)
	q := sqlc.New(pool)
	if err := q.UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
		IdentityID: pgID, Question: "first pet?", AnswerHash: "hash", AnswerHashVersion: "v1",
	}); err != nil {
		t.Fatalf("seed recovery: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identity_auth_links (identity_id, authula_user_id) VALUES ($1, $2)`, pgID, "reset-"+id.String()); err != nil {
		t.Fatalf("seed auth link: %v", err)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, id.String(), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id) VALUES ($1, $2)`, telegramUserID, pgID)
		return err
	}); err != nil {
		t.Fatalf("seed telegram link: %v", err)
	}

	got, err := lookupPasswordResetTelegramUserID(ctx, pool, id)
	if err != nil || got != telegramUserID {
		t.Fatalf("password reset lookup = %d, %v; want %d", got, err, telegramUserID)
	}
	row, err := q.LookupRecoveryByEmail(ctx, email)
	if err != nil || row.TelegramUserID != telegramUserID {
		t.Fatalf("recovery lookup = %d, %v; want %d", row.TelegramUserID, err, telegramUserID)
	}
}
