//go:build db_integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestMigration0023IdentityRecoveryRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_migrate0023_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema: %v", err)
	}

	migrateURL := dsn("aura_migrate", freshDB)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("full Migrate up on fresh db: %v", err)
	}
	headVersion := currentMigrationVersion(t, ctx, freshAdmin)
	if headVersion < 23 {
		t.Fatalf("full Migrate up reached version %d, want at least 23", headVersion)
	}
	stepsToBefore0023 := headVersion - 22
	if err := MigrateSteps(ctx, migrateURL, -stepsToBefore0023); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0023: %v", -stepsToBefore0023, err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0023: %v", err)
	}

	migratePool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migrate pool: %v", err)
	}
	defer migratePool.Close()
	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	for _, table := range []string{
		"aura.identity_recovery",
		"aura.password_reset_challenges",
		"aura.password_reset_tokens",
		"aura.identity_recovery_audit",
	} {
		var exists bool
		if err := migratePool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("%s missing after migrate up", table)
		}
	}

	seedID := pgtype.UUID{Bytes: [16]byte{0x23, 0x01}, Valid: true}
	otherID := pgtype.UUID{Bytes: [16]byte{0x23, 0x02}, Valid: true}
	channelID := pgtype.UUID{Bytes: [16]byte{0x23, 0x03}, Valid: true}
	if err := execStatements(ctx, app, []statement{
		{sql: `INSERT INTO aura.identities (id, name, kind) VALUES
    ($1, 'recovery-0023@example.test', 'user'),
    ($2, 'recovery-0023-other@example.test', 'user'),
    ($3, 'RECOVERY-0023@example.test', 'channel')`, args: []any{seedID, otherID, channelID}},
		{sql: `INSERT INTO aura.identity_auth_links (identity_id, authula_user_id, created_at)
    VALUES
        ($1, 'authula-recovery-0023-old', now() - interval '2 hours'),
        ($1, 'authula-recovery-0023-new', now())`, args: []any{seedID}},
		{sql: `INSERT INTO aura.identity_auth_links (identity_id, authula_user_id, created_at)
    VALUES ($1, 'authula-recovery-0023-channel', now() + interval '1 hour')`, args: []any{channelID}},
		{sql: `INSERT INTO aura.identity_recovery (identity_id, question, answer_hash, answer_hash_version)
    VALUES ($1, 'Question?', 'answer-hash', 'v1')`, args: []any{seedID}},
		{sql: `INSERT INTO aura.identity_recovery (identity_id, question, answer_hash, answer_hash_version)
    VALUES ($1, 'Channel question?', 'channel-answer-hash', 'v1')`, args: []any{channelID}},
		{sql: `INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id, username, first_name, added_at)
    VALUES
        (230101, $1, 'old_recovery_0023', 'Old', now() - interval '1 hour'),
        (230102, $1, 'new_recovery_0023', 'New', now())`, args: []any{seedID}},
		{sql: `INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id, username, first_name, added_at, last_seen_at)
    VALUES (230999, $1, 'channel_recovery_0023', 'Channel', now() + interval '1 hour', now() + interval '1 hour')`, args: []any{channelID}},
	}); err != nil {
		t.Fatalf("seed recovery invariant fixtures: %v", err)
	}

	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.identities (id, name, kind)
VALUES ('00000000-0000-0000-0000-000000002303', 'RECOVERY-0023@example.test', 'user')
`), "23505")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', -1, 5)
`, seedID), "23514")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', 0, 11)
`, seedID), "23514")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', now() + interval '5 minutes', 6, 5)
`, seedID), "23514")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, created_at, expires_at, attempt_count, max_attempts
) VALUES ($1, 'code-hash', '2026-01-01 00:00:00+00', '2026-01-01 00:00:00+00', 0, 5)
`, seedID), "23514")

	var challengeID pgtype.UUID
	if err := app.QueryRow(ctx, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, created_at, expires_at, max_attempts
) VALUES ($1, 'code-hash', now() - interval '2 minutes', now() + interval '5 minutes', 5)
RETURNING id
`, seedID).Scan(&challengeID); err != nil {
		t.Fatalf("insert valid challenge: %v", err)
	}
	var exhaustedChallengeID pgtype.UUID
	if err := app.QueryRow(ctx, `
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, created_at, expires_at, attempt_count, max_attempts
) VALUES ($1, 'exhausted-code-hash', now(), now() + interval '5 minutes', 5, 5)
RETURNING id
`, otherID).Scan(&exhaustedChallengeID); err != nil {
		t.Fatalf("insert exhausted challenge: %v", err)
	}

	q := sqlc.New(app)
	if _, err := q.GetActivePasswordResetChallenge(ctx, otherID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("get exhausted active challenge error = %v, want pgx.ErrNoRows", err)
	}
	if err := q.IncrementPasswordResetChallengeAttempts(ctx, exhaustedChallengeID); err != nil {
		t.Fatalf("increment exhausted challenge attempts: %v", err)
	}
	requireAttemptCount(t, ctx, app, "aura.password_reset_challenges", "id", exhaustedChallengeID, 5)

	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, max_attempts
) VALUES ('mismatched-token-0023', $1, $2, now() + interval '5 minutes', 3)
`, challengeID, otherID), "23503")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('bad-attempt-token-0023', $1, $2, now() + interval '5 minutes', -1, 3)
`, challengeID, seedID), "23514")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('bad-max-token-0023', $1, $2, now() + interval '5 minutes', 0, 11)
`, challengeID, seedID), "23514")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('over-attempt-token-0023', $1, $2, now() + interval '5 minutes', 4, 3)
`, challengeID, seedID), "23514")
	requireSQLState(t, execErr(ctx, app, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, created_at, expires_at, attempt_count, max_attempts
) VALUES ('expired-token-0023', $1, $2, '2026-01-01 00:00:00+00', '2026-01-01 00:00:00+00', 0, 3)
`, challengeID, seedID), "23514")
	if _, err := app.Exec(ctx, `
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, attempt_count, max_attempts
) VALUES ('exhausted-token-0023', $1, $2, now() + interval '5 minutes', 3, 3)
`, challengeID, seedID); err != nil {
		t.Fatalf("insert exhausted token: %v", err)
	}
	if err := q.IncrementPasswordResetTokenAttempts(ctx, "exhausted-token-0023"); err != nil {
		t.Fatalf("increment exhausted token attempts: %v", err)
	}
	requireAttemptCount(t, ctx, app, "aura.password_reset_tokens", "token_hash", "exhausted-token-0023", 3)

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
	requireSQLState(t, execErr(ctx, migratePool,
		`UPDATE aura.identity_recovery_audit SET event = 'mutated' WHERE id = $1`, auditRow.ID), "42501")
	requireSQLState(t, execErr(ctx, migratePool,
		`DELETE FROM aura.identity_recovery_audit WHERE id = $1`, auditRow.ID), "42501")
	requireSQLState(t, execErr(ctx, migratePool,
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

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("post-round-trip Migrate to HEAD: %v", err)
	}
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0023 reversible), got %d", n)
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

func requireAttemptCount(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, table, key string, value any, want int32) {
	t.Helper()
	var got int32
	if err := pool.QueryRow(ctx,
		fmt.Sprintf("SELECT attempt_count FROM %s WHERE %s = $1", table, key),
		value,
	).Scan(&got); err != nil {
		t.Fatalf("read %s attempt_count: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s attempt_count = %d, want %d", table, got, want)
	}
}
