//go:build db_integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestMigration0023IdentityRecoveryRoundTrip(t *testing.T) {
	ctx := context.Background()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migrate pool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, table := range []string{
		"aura.identity_recovery",
		"aura.password_reset_challenges",
		"aura.password_reset_tokens",
		"aura.identity_recovery_audit",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("%s missing after migrate up", table)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate down 0023: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate re-up 0023: %v", err)
	}

	seedID := pgtype.UUID{Bytes: [16]byte{0x23, 0x01}, Valid: true}
	otherID := pgtype.UUID{Bytes: [16]byte{0x23, 0x02}, Valid: true}
	if err := execStatements(ctx, pool, []statement{
		{sql: `DELETE FROM aura.telegram_accounts WHERE telegram_user_id IN (230101, 230102)`},
		{sql: `DELETE FROM aura.identities WHERE id IN ($1, $2)`, args: []any{seedID, otherID}},
		{sql: `INSERT INTO aura.identities (id, name, kind) VALUES
    ($1, 'recovery-0023@example.test', 'user'),
    ($2, 'recovery-0023-other@example.test', 'user')`, args: []any{seedID, otherID}},
		{sql: `INSERT INTO aura.identity_auth_links (identity_id, authula_user_id, created_at)
    VALUES
        ($1, 'authula-recovery-0023-old', now() - interval '2 hours'),
        ($1, 'authula-recovery-0023-new', now())`, args: []any{seedID}},
		{sql: `INSERT INTO aura.identity_recovery (identity_id, question, answer_hash, answer_hash_version)
    VALUES ($1, 'Question?', 'answer-hash', 'v1')`, args: []any{seedID}},
		{sql: `INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id, username, first_name, added_at)
    VALUES
        (230101, $1, 'old_recovery_0023', 'Old', now() - interval '1 hour'),
        (230102, $1, 'new_recovery_0023', 'New', now())`, args: []any{seedID}},
	}); err != nil {
		t.Fatalf("seed recovery invariant fixtures: %v", err)
	}

	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', -1, 5)
`, seedID), "23514")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', 0, 11)
`, seedID), "23514")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', 6, 5)
`, seedID), "23514")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, created_at, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', '2026-01-01 00:00:00+00', '2026-01-01 00:00:00+00', 0, 5)
`, seedID), "23514")

	var challengeID pgtype.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', 5)
RETURNING id
`, seedID).Scan(&challengeID); err != nil {
		t.Fatalf("insert valid challenge: %v", err)
	}
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, max_attempts
) VALUES ('mismatched-token-0023', $1, $2, now() + interval '5 minutes', 3)
`, challengeID, otherID), "23503")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('bad-attempt-token-0023', $1, $2, now() + interval '5 minutes', -1, 3)
`, challengeID, seedID), "23514")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('bad-max-token-0023', $1, $2, now() + interval '5 minutes', 0, 11)
`, challengeID, seedID), "23514")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('over-attempt-token-0023', $1, $2, now() + interval '5 minutes', 4, 3)
`, challengeID, seedID), "23514")
	requireSQLState(t, execErr(ctx, pool, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, created_at, expires_at, attempt_count, max_attempts
) VALUES ('expired-token-0023', $1, $2, '2026-01-01 00:00:00+00', '2026-01-01 00:00:00+00', 0, 3)
`, challengeID, seedID), "23514")

	q := sqlc.New(pool)
	auditRow, err := q.InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
		IdentityID: seedID,
		Event:      "test-event",
		Metadata:   nil,
	})
	if err != nil {
		t.Fatalf("insert audit with nil metadata: %v", err)
	}
	if string(auditRow.Metadata) != "{}" {
		t.Fatalf("nil audit metadata default = %s, want {}", string(auditRow.Metadata))
	}
	requireSQLState(t, execErr(ctx, pool,
		`UPDATE aura.identity_recovery_audit SET event = 'mutated' WHERE id = $1`, auditRow.ID), "42501")
	requireSQLState(t, execErr(ctx, pool,
		`DELETE FROM aura.identity_recovery_audit WHERE id = $1`, auditRow.ID), "42501")
	requireSQLState(t, execErr(ctx, pool,
		`TRUNCATE aura.identity_recovery_audit`), "42501")

	lookup, err := q.LookupRecoveryByEmail(ctx, "RECOVERY-0023@EXAMPLE.TEST")
	if err != nil {
		t.Fatalf("lookup recovery by email: %v", err)
	}
	if lookup.TelegramUserID != 230102 {
		t.Fatalf("lookup telegram_user_id = %d, want most recent 230102", lookup.TelegramUserID)
	}
	if lookup.AuthulaUserID != "authula-recovery-0023-new" {
		t.Fatalf("lookup authula_user_id = %q, want newest authula-recovery-0023-new", lookup.AuthulaUserID)
	}
}

type sqlExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type statement struct {
	sql  string
	args []any
}

func execStatements(ctx context.Context, pool sqlExecer, statements []statement) error {
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}

func execErr(ctx context.Context, pool sqlExecer, sql string, args ...any) error {
	_, err := pool.Exec(ctx, sql, args...)
	return err
}

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected SQLSTATE %s, got nil error", code)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected SQLSTATE %s, got non-Postgres error: %v", code, err)
	}
	if pgErr.Code != code {
		t.Fatalf("SQLSTATE = %s (%s), want %s", pgErr.Code, pgErr.Message, code)
	}
}
