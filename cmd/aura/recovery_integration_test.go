//go:build db_integration

// Live break-glass proof (MUSR-06 / D-16): on the real Postgres stack,
// mintBreakGlassToken creates+consumes a parent challenge and inserts a hashed
// reset token reusing the 0023 infra, the returned PLAINTEXT token resolves as a
// WORKING token (its hash validates against aura.password_reset_tokens), and a
// neutral audit row is appended. A non-existent identity fails closed (FK).
//
// Env (no-skip-as-green — fails loud under $CI when unset): POSTGRES_PASSWORD +
// AURA_DB_URL + AURA_DB_MIGRATE_URL on the live stack (see
// internal/skills/audit_store_integration_test.go header for the invocation).
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

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identity"
)

func recoveryEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

func TestMintBreakGlassTokenRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pwd := recoveryEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, recoveryEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
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

	// Seed a fresh isolated user identity.
	newID := uuid.New()
	name := "recover-" + newID.String()[:8] + "@example.test"
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

	// Resolve-by-name is the CLI entry path.
	store := identity.New(pool)
	resolved, err := store.GetIdentityByName(ctx, name)
	if err != nil {
		t.Fatalf("GetIdentityByName(%q): %v", name, err)
	}
	if resolved.ID != newID.String() {
		t.Fatalf("resolved ID = %q, want %q", resolved.ID, newID.String())
	}

	// Mint the break-glass token.
	token, err := mintBreakGlassToken(ctx, pool, resolved.ID, passwordResetTestPepper)
	if err != nil {
		t.Fatalf("mintBreakGlassToken: %v", err)
	}
	if token == "" {
		t.Fatal("mintBreakGlassToken returned an empty token")
	}

	// The returned plaintext token is a WORKING token: its hash validates.
	gotID, err := resolveResetTokenHash(ctx, sqlc.New(pool), agui.HashLookupToken(token, passwordResetTestPepper))
	if err != nil {
		t.Fatalf("resolveResetTokenHash on minted token: %v", err)
	}
	if gotID != newID.String() {
		t.Fatalf("minted token resolves to %q, want %q", gotID, newID.String())
	}

	// The plaintext token itself is never stored (only its hash is).
	var plaintextRows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.password_reset_tokens WHERE token_hash = $1`, token,
	).Scan(&plaintextRows); err != nil {
		t.Fatalf("count plaintext rows: %v", err)
	}
	if plaintextRows != 0 {
		t.Fatalf("found %d rows keyed on the PLAINTEXT token; it must be hashed at rest", plaintextRows)
	}

	// A neutral break-glass audit row was appended.
	var auditRows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.identity_recovery_audit WHERE identity_id = $1 AND event = $2`,
		pgtype.UUID{Bytes: newID, Valid: true}, breakGlassAuditEvent,
	).Scan(&auditRows); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditRows < 1 {
		t.Fatalf("break-glass audit rows = %d, want >= 1", auditRows)
	}

	// A non-existent identity fails closed (challenge FK to aura.identities).
	if _, err := mintBreakGlassToken(ctx, pool, uuid.New().String(), passwordResetTestPepper); err == nil {
		t.Fatal("mintBreakGlassToken for an unknown identity succeeded, want fail-closed error")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}
