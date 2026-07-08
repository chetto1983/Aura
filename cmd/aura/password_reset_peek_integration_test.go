//go:build db_integration

// Live proof (password-reset caveat 2 — two-phase verify): on the real Postgres stack the
// recoveryStoreAdapter.PeekChallenge validates a live Telegram code WITHOUT consuming the
// challenge, so the security question can be revealed and the SAME code still verifies
// afterward. A wrong code is denied and rate-limited (attempt_count++), never revealing
// anything. This exercises the real GetActivePasswordResetChallenge SQL path end-to-end.
//
// Env (no-skip-as-green — fails loud under $CI when unset): POSTGRES_PASSWORD +
// AURA_DB_URL + AURA_DB_MIGRATE_URL on the live stack.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestPeekChallengeDoesNotConsumeLiveDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pwd := recoveryEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := recoveryEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := recoveryEnvOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	newID := uuid.New()
	name := "peek-" + newID.String()[:8] + "@example.test"
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		pgtype.UUID{Bytes: newID, Valid: true}, name,
	); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM aura.identities WHERE id = $1`, pgtype.UUID{Bytes: newID, Valid: true})
	})

	const code = "known-peek-code-123456"
	codeHash, err := agui.HashOpaqueSecret(code)
	if err != nil {
		t.Fatalf("HashOpaqueSecret: %v", err)
	}
	if _, err := sqlc.New(pool).InsertPasswordResetChallenge(ctx, sqlc.InsertPasswordResetChallengeParams{
		IdentityID:  pgtype.UUID{Bytes: newID, Valid: true},
		CodeHash:    codeHash,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		MaxAttempts: passwordResetChallengeMaxAttempts,
	}); err != nil {
		t.Fatalf("InsertPasswordResetChallenge: %v", err)
	}

	adapter := recoveryStoreAdapter{pool: pool}

	// A correct code is accepted and leaves the attempt counter untouched.
	if err := adapter.PeekChallenge(ctx, newID.String(), code); err != nil {
		t.Fatalf("PeekChallenge(correct code): %v", err)
	}
	if got := challengeAttemptCount(ctx, t, pool, newID); got != 0 {
		t.Fatalf("attempt_count after correct peek = %d, want 0 (peek must not rate-limit a valid code)", got)
	}

	// A wrong code is denied and rate-limited (attempt_count increments).
	if err := adapter.PeekChallenge(ctx, newID.String(), "wrong-code"); !errors.Is(err, agui.ErrPasswordResetDenied) {
		t.Fatalf("PeekChallenge(wrong code) = %v, want ErrPasswordResetDenied", err)
	}
	if got := challengeAttemptCount(ctx, t, pool, newID); got != 1 {
		t.Fatalf("attempt_count after wrong peek = %d, want 1 (wrong code must be rate-limited)", got)
	}

	// The peek did NOT consume the challenge: the same code still verifies into a reset token.
	token, err := adapter.VerifyChallenge(ctx, newID.String(), code)
	if err != nil {
		t.Fatalf("VerifyChallenge after peek: %v", err)
	}
	if token == "" {
		t.Fatal("VerifyChallenge after peek returned an empty token; the peek wrongly consumed the challenge")
	}
}

func challengeAttemptCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, identityID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT attempt_count FROM aura.password_reset_challenges WHERE identity_id = $1`,
		pgtype.UUID{Bytes: identityID, Valid: true},
	).Scan(&n); err != nil {
		t.Fatalf("read attempt_count: %v", err)
	}
	return n
}
