//go:build db_integration

// Live break-glass proof (R1/R2/R4/R6, D-06/D-07/D-08). The full offline flow runs
// against a SELF-PROVISIONED, disposable database (D-07): the harness connects as
// superuser to the `postgres` maintenance DB, REFUSES a name of `aura` (D-08 — the
// exact 37D-05 coverage-gate footgun that wiped the live operator), CREATEs a throwaway
// owned by aura_migrate, migrates + seeds it, and DROPs it WITH (FORCE) on cleanup. The
// COMMAND itself carries no such refusal (it must run against live `aura`); the guard
// lives only here.
//
// Placement is load-bearing (D-09): this file sits in internal/breakglass so
// scripts/coverage_gate.sh (`./internal/...`) actually EXECUTES it and it counts toward
// the 85% owned-surface floor — a cmd/aura db_integration test is only go-vet-compiled,
// never run (RESEARCH Pitfall 1).
//
// Env (no-skip-as-green — t.Fatal under $CI when unset, skip locally): POSTGRES_PASSWORD
// + AURA_DB_MIGRATE_URL + AURA_DB_URL + AURA_AUTHULA_SECRET on the live stack.
package breakglass

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/webauth"
)

// Fixed sentinels so the no-leak assertion can prove neither the new password nor the
// recovery answer ever reaches an slog sink.
const (
	bgNewPassword = "S3nt1nel-Br34kGl4ss-New-Pw"
	bgOldPassword = "S3nt1nel-Br34kGl4ss-Old-Pw"
	bgAnswer      = "S3nt1nel-Br34kGl4ss-Answer"
	bgQuestion    = "break-glass integration question"
)

// seedTelegramUserID is a fixed non-colliding Telegram id for the seeded delivery row
// (each subtest owns a fresh throwaway DB, so a constant never collides).
const seedTelegramUserID int64 = 7700000001

// throwawayNameRe is the injection guard on the throwaway DB identifier: even though the
// harness generates the name, a name interpolated into DDL must be a safe identifier.
var throwawayNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,62}$`)

// recoveryEnvOrSkip is the sanctioned no-skip-as-green env guard (copied verbatim from
// cmd/aura/recovery_integration_test.go:31-41): t.Fatal under $CI when a required var is
// unset, skip locally.
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

// bgEnv is the resolved connection context. It reads ALL four required vars through the
// no-skip guard (R6): AURA_DB_URL/AURA_DB_MIGRATE_URL gate the guard even though the
// harness composes its own throwaway DSNs; host/port derive from PGHOST/PGPORT.
type bgEnv struct {
	pwd    string
	secret string
	host   string
	port   string
}

func readBGEnv(t *testing.T) bgEnv {
	t.Helper()
	pwd := recoveryEnvOrSkip(t, "POSTGRES_PASSWORD")
	recoveryEnvOrSkip(t, "AURA_DB_MIGRATE_URL") // required under $CI (no-skip-as-green)
	recoveryEnvOrSkip(t, "AURA_DB_URL")         // required under $CI (no-skip-as-green)
	secret := recoveryEnvOrSkip(t, "AURA_AUTHULA_SECRET")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return bgEnv{pwd: pwd, secret: secret, host: host, port: port}
}

// dsn composes a Postgres URL for role@database (mirrors recovery_integration_test.go:59;
// the live stack password is URL-safe).
func (e bgEnv) dsn(role, database string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, e.pwd, e.host, e.port, database)
}

// quoteIdent double-quotes a validated SQL identifier (CREATE/DROP DATABASE take no bind
// params); mirrors internal/db/migrate.go.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// provisionThrowawayDB CREATE/DROPs a disposable database as superuser on the `postgres`
// maintenance DB (D-07), refusing the live `aura` name (D-08). It returns the DB name and
// registers a t.Cleanup that DROPs it WITH (FORCE).
func provisionThrowawayDB(t *testing.T, ctx context.Context, env bgEnv) string {
	t.Helper()
	dbName := strings.TrimSpace(os.Getenv("AURA_BREAKGLASS_TEST_DB"))
	if dbName == "" {
		dbName = "aura_bg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	// D-08: the disposable tier CREATE/DROPs this DB; a name of `aura` is the 37D-05 footgun.
	if dbName == "aura" {
		t.Fatalf("refusing throwaway DB name %q: the db_integration tier CREATE/DROPs it (D-08)", dbName)
	}
	if !throwawayNameRe.MatchString(dbName) {
		t.Fatalf("throwaway DB name %q must match %s (identifier-injection guard)", dbName, throwawayNameRe)
	}

	admin, err := pgx.Connect(ctx, env.dsn("aura", "postgres"))
	if err != nil {
		t.Fatalf("connect superuser to the postgres maintenance DB: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// aura_migrate must exist to own the disposable DB (mirrors coverage_docker.sh:54-56).
	var hasRole bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='aura_migrate')`).Scan(&hasRole); err != nil {
		t.Fatalf("probe aura_migrate role: %v", err)
	}
	if !hasRole {
		if _, err := admin.Exec(ctx, "CREATE ROLE aura_migrate WITH LOGIN PASSWORD '"+strings.ReplaceAll(env.pwd, "'", "''")+"'"); err != nil {
			t.Fatalf("create aura_migrate role: %v", err)
		}
	}

	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoteIdent(dbName)+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop pre-existing throwaway DB %q: %v", dbName, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoteIdent(dbName)+" OWNER aura_migrate"); err != nil {
		t.Fatalf("create throwaway DB %q: %v", dbName, err)
	}
	t.Logf("provisioned disposable break-glass DB %q (dropped on cleanup)", dbName)

	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(cctx, env.dsn("aura", "postgres"))
		if err != nil {
			t.Logf("cleanup: reconnect to drop throwaway DB %q: %v", dbName, err)
			return
		}
		defer func() { _ = conn.Close(cctx) }()
		if _, err := conn.Exec(cctx, "DROP DATABASE IF EXISTS "+quoteIdent(dbName)+" WITH (FORCE)"); err != nil {
			t.Logf("cleanup: drop throwaway DB %q: %v", dbName, err)
		}
	})
	return dbName
}

// setupBreakglassDB provisions a throwaway DB, ensures roles, migrates, opens the aura
// pool, and constructs the offline Authula provider — returning the pool + CoreServices
// pointed at the disposable DB. All lifecycle is registered on t.Cleanup.
func setupBreakglassDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *authulaservices.CoreServices) {
	t.Helper()
	env := readBGEnv(t)
	dbName := provisionThrowawayDB(t, ctx, env)

	if err := db.EnsureRoles(ctx, env.dsn("aura", dbName), env.pwd); err != nil {
		t.Fatalf("EnsureRoles on throwaway %q: %v", dbName, err)
	}
	if _, err := db.Migrate(ctx, env.dsn("aura_migrate", dbName)); err != nil {
		t.Fatalf("Migrate throwaway %q: %v", dbName, err)
	}
	appURL := env.dsn("aura_app", dbName)
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open throwaway %q: %v", dbName, err)
	}
	t.Cleanup(pool.Close)

	provider, err := NewAuthula(appURL, env.secret)
	if err != nil {
		t.Fatalf("construct offline Authula provider: %v", err)
	}
	// provider.Close stops Authula's expiry workers (goleak-clean under -race).
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Logf("close Authula provider: %v", err)
		}
	})
	return pool, provider.CoreServices()
}

// seedOperatorLockout seeds the exact lockout state: one kind='user' operator with an
// Authula email account (bgOldPassword), an identity_auth_links row, a telegram_accounts
// delivery row, one live session, and NO identity_recovery row. It returns the aura
// identity id and the Authula user id.
func seedOperatorLockout(t *testing.T, ctx context.Context, pool *pgxpool.Pool, core *authulaservices.CoreServices, email string) (identityID, authulaUserID string) {
	t.Helper()
	user, err := core.UserService.Create(ctx, email, email, true, nil, nil)
	if err != nil {
		t.Fatalf("create Authula user: %v", err)
	}
	oldHash, err := core.PasswordService.Hash(bgOldPassword)
	if err != nil {
		t.Fatalf("hash seed password: %v", err)
	}
	if _, err := core.AccountService.Create(ctx, user.ID, email, authulamodels.AuthProviderEmail.String(), &oldHash); err != nil {
		t.Fatalf("create Authula account: %v", err)
	}

	identityID = seedUserIdentity(t, ctx, pool, email)
	if err := webauth.NewIdentityLinker(pool).LinkOperator(ctx, identityID, user.ID); err != nil {
		t.Fatalf("link operator identity to Authula user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id, username) VALUES ($1, $2, $3)`,
		seedTelegramUserID, mustUUID(t, identityID), "operator",
	); err != nil {
		t.Fatalf("seed telegram account: %v", err)
	}

	// Seed one live session so the sessions==0 post-run assertion is meaningful.
	token, err := core.TokenService.Generate()
	if err != nil {
		t.Fatalf("generate seed session token: %v", err)
	}
	if _, err := core.SessionService.Create(ctx, user.ID, core.TokenService.Hash(token), nil, nil, time.Hour); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return identityID, user.ID
}

// seedUserIdentity inserts a bare kind='user' identity named `name` and returns its id.
func seedUserIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	idUUID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		pgtype.UUID{Bytes: idUUID, Valid: true}, name,
	); err != nil {
		t.Fatalf("seed identity %q: %v", name, err)
	}
	return idUUID.String()
}

func mustUUID(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", id, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

// countRows runs a scalar count query on the aura pool.
func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

// verifyStoredPassword re-reads the operator's Authula account and reports whether
// PasswordService.Verify accepts the candidate against the stored hash (the R1 argon2
// round-trip through Authula's OWN Verify).
func verifyStoredPassword(t *testing.T, ctx context.Context, core *authulaservices.CoreServices, authulaUserID, candidate string) bool {
	t.Helper()
	account, err := core.AccountService.GetByUserIDAndProvider(ctx, authulaUserID, authulamodels.AuthProviderEmail.String())
	if err != nil {
		t.Fatalf("re-read operator account: %v", err)
	}
	if account == nil || account.Password == nil {
		t.Fatalf("operator account or password is nil after reset")
	}
	return core.PasswordService.Verify(candidate, *account.Password)
}

// TestRecoverOperatorHappyPath proves the full success flow on a disposable DB: the reset
// password is Authula-Verify-accepted (R1), every session is gone (prohibition), the
// re-seed restores LookupRecoveryByEmail + is idempotent (R4), exactly one neutral audit
// row lands (D-06), and no secret reaches an slog sink (prohibition).
func TestRecoverOperatorHappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	pool, core := setupBreakglassDB(t, ctx)
	email := "operator-" + uuid.NewString()[:8] + "@example.test"
	identityID, authulaUserID := seedOperatorLockout(t, ctx, pool, core, email)

	// Pre-condition: the lockout — LookupRecoveryByEmail denies (INNER JOIN, no recovery row).
	if _, err := sqlc.New(pool).LookupRecoveryByEmail(ctx, email); err == nil {
		t.Fatal("pre-run LookupRecoveryByEmail returned a row; expected the lockout (pgx.ErrNoRows)")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pre-run LookupRecoveryByEmail: unexpected error %v (want pgx.ErrNoRows)", err)
	}
	if got := verifyStoredPassword(t, ctx, core, authulaUserID, bgOldPassword); !got {
		t.Fatal("seed sanity: the OLD password does not verify before the run")
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM authula.sessions WHERE user_id=$1`, authulaUserID); got != 1 {
		t.Fatalf("seed sanity: sessions = %d, want 1", got)
	}

	// Capture slog for the no-leak prohibition across the full run.
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	gotID, err := RecoverOperator(ctx, Deps{Pool: pool, Core: core}, Secrets{
		Password: bgNewPassword, Question: bgQuestion, Answer: bgAnswer,
	})
	if err != nil {
		t.Fatalf("RecoverOperator: %v", err)
	}
	if gotID != identityID {
		t.Fatalf("RecoverOperator returned id %q, want the operator %q", gotID, identityID)
	}

	// R1: the new password is Verify-accepted; the old one no longer is.
	if !verifyStoredPassword(t, ctx, core, authulaUserID, bgNewPassword) {
		t.Fatal("R1: Authula PasswordService.Verify rejects the reset password (argon2 envelope mismatch?)")
	}
	if verifyStoredPassword(t, ctx, core, authulaUserID, bgOldPassword) {
		t.Fatal("R1: the OLD password still verifies after the reset")
	}
	// Prohibition: no prior session survives.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM authula.sessions WHERE user_id=$1`, authulaUserID); got != 0 {
		t.Fatalf("sessions after reset = %d, want 0", got)
	}

	// R4: the re-seed restores the forgot-password lookup and stores a non-empty hash+version.
	rec, err := sqlc.New(pool).GetIdentityRecoveryByIdentity(ctx, mustUUID(t, identityID))
	if err != nil {
		t.Fatalf("GetIdentityRecoveryByIdentity after re-seed: %v", err)
	}
	if rec.AnswerHash == "" || rec.AnswerHashVersion == "" {
		t.Fatalf("re-seeded recovery has empty hash/version: %+v", rec)
	}
	if _, err := sqlc.New(pool).LookupRecoveryByEmail(ctx, email); err != nil {
		t.Fatalf("post-run LookupRecoveryByEmail: %v (want a row)", err)
	}

	// D-06: exactly one neutral audit row, event set, metadata '{}'.
	assertSingleNeutralAudit(t, ctx, pool, identityID)

	// No-leak: neither secret reached the slog sink.
	if s := logBuf.String(); strings.Contains(s, bgNewPassword) || strings.Contains(s, bgAnswer) {
		t.Fatalf("a secret leaked into the slog sink: %q", s)
	}

	// R4 idempotency: a second run leaves exactly one identity_recovery row (upsert-in-place).
	if _, err := RecoverOperator(ctx, Deps{Pool: pool, Core: core}, Secrets{
		Password: bgNewPassword, Question: bgQuestion, Answer: bgAnswer,
	}); err != nil {
		t.Fatalf("second RecoverOperator (idempotency): %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery WHERE identity_id=$1`, mustUUID(t, identityID)); got != 1 {
		t.Fatalf("identity_recovery rows after two runs = %d, want 1 (upsert)", got)
	}
}

// assertSingleNeutralAudit asserts exactly one operator_password_recovered audit row with
// metadata '{}' for the identity (D-06).
func assertSingleNeutralAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT event, metadata::text FROM aura.identity_recovery_audit WHERE identity_id=$1`,
		mustUUID(t, identityID))
	if err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	defer rows.Close()
	var events []string
	for rows.Next() {
		var event, metadata string
		if err := rows.Scan(&event, &metadata); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		if event != auditEventOperatorRecovered {
			t.Fatalf("audit event = %q, want %q", event, auditEventOperatorRecovered)
		}
		if metadata != "{}" {
			t.Fatalf("audit metadata = %q, want '{}' (no secret)", metadata)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", len(events))
	}
}
