//go:build db_integration

// The live harness behind onboarding_provision_integration_test.go: the three real-store
// legs (aura, telegram, recovery), the stateful Authula fake, the fault-injection wrappers
// and the disposable-database environment. Split out of that file on 2026-08-02 when
// identity-scoping the grant seeds pushed it past the 600-LOC cap (CLAUDE.md, no god
// class). The assertions live next door; this file is only the scaffolding they stand on.
//
// Why the Authula leg is a fake and not the real provider — goleak, and where the real one
// IS proven — is documented at the top of onboarding_provision_integration_test.go.

package agui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
)

// --- REAL aura-leg + telegram adapters over the live pool (package agui can import
// db/sqlc/identity; the telegram leg uses raw SQL to avoid the telegram→agui cycle) ---

type liveAuraLeg struct{ pool *pgxpool.Pool }

func (a liveAuraLeg) CreateIdentityWithGrants(ctx context.Context, p AuraLegParams) (string, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	q := sqlc.New(tx)
	newID := uuid.New()
	if _, err := q.CreateIdentity(ctx, sqlc.CreateIdentityParams{
		ID: pgtype.UUID{Bytes: newID, Valid: true}, Name: p.IdentityName, Kind: "user",
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrOnboardingDuplicate
		}
		return "", err
	}
	// Mirrors cmd/aura/serve_onboarding.go's adapter: aura.capability_grants is fail-closed
	// as of migration 0087, and the identity being granted is only minted one statement
	// above, so the tx has to be scoped from the inside.
	if err := db.SetTxIdentity(ctx, tx, newID.String()); err != nil {
		return "", err
	}
	for _, c := range p.Capabilities {
		if err := identity.ValidateCapabilityName(c); err != nil {
			return "", ErrOnboardingEscalation
		}
		if err := q.GrantCapability(ctx, sqlc.GrantCapabilityParams{
			IdentityID: pgtype.UUID{Bytes: newID, Valid: true}, Capability: c,
		}); err != nil {
			return "", err
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO aura.identity_auth_links (identity_id, authula_user_id) VALUES ($1::uuid,$2)
		 ON CONFLICT (authula_user_id) DO UPDATE SET identity_id = EXCLUDED.identity_id`,
		newID.String(), p.AuthulaUserID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	committed = true
	return newID.String(), nil
}

func (a liveAuraLeg) WriteAuditRow(ctx context.Context, p AuraLegParams, newID string) error {
	return db.WithTx(ctx, a.pool, func(q *sqlc.Queries) error {
		return identity.InsertIdentityAuditTx(ctx, q, identity.IdentityAuditInsert{
			ActorIdentityID: p.ActorIdentityID, NewIdentityID: newID,
			NewIdentityName: p.IdentityName, GrantedCapabilities: p.Capabilities,
			AuthulaUserID: p.AuthulaUserID,
		})
	})
}

func (a liveAuraLeg) DeleteIdentity(ctx context.Context, name string) error {
	return identity.New(a.pool).DeleteIdentity(ctx, name)
}

// liveTelegram is a raw-SQL telegram mint over aura.telegram_setup_pending (the agui test
// can NOT import internal/channels/telegram — that package imports agui, a cycle).
type liveTelegram struct{ pool *pgxpool.Pool }

func (a liveTelegram) InsertPending(ctx context.Context, token, identityID string, expiresAt time.Time) error {
	_, err := a.pool.Exec(ctx,
		`INSERT INTO aura.telegram_setup_pending (onboarding_token, identity_id, generated_by, expires_at)
		 VALUES ($1, $2::uuid, $3, $4)`,
		token, identityID, "onboarding-wizard-test", expiresAt)
	return err
}

func (a liveTelegram) PendingConsumed(ctx context.Context, token string) (bool, error) {
	var consumed pgtype.Timestamptz
	err := a.pool.QueryRow(ctx,
		`SELECT consumed_at FROM aura.telegram_setup_pending WHERE onboarding_token=$1`, token).Scan(&consumed)
	if err != nil {
		return false, err
	}
	return consumed.Valid, nil
}

func (a liveTelegram) DeletePending(ctx context.Context, token string) error {
	_, err := a.pool.Exec(ctx, `DELETE FROM aura.telegram_setup_pending WHERE onboarding_token=$1`, token)
	return err
}

type liveRecovery struct{ pool *pgxpool.Pool }

func (a liveRecovery) UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error {
	id, err := uuid.Parse(identityID)
	if err != nil {
		return err
	}
	return sqlc.New(a.pool).UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
		IdentityID: pgtype.UUID{Bytes: id, Valid: true}, Question: question,
		AnswerHash: answerHash, AnswerHashVersion: answerHashVersion,
	})
}

// statefulAuthula is the in-memory Authula leg (goleak-clean — no provider goroutines). It
// records created/deleted users so COMP_B's zero-orphan-user property is provable, with
// optional fault injection at CreateUser (B1) / CreateAccount (B2). It mirrors the real
// adapter's ordered semantics (Hash → CreateUser → CreateAccount; DeleteUser cascades the
// account). The real CoreServices Leg B is proven in the webauth integration tests.
type statefulAuthula struct {
	mu             sync.Mutex
	users          map[string]string // userID -> email
	emails         map[string]string // email -> userID
	enforced       []string          // userIDs the D-15 first-login policy was applied to
	failCreateUser bool
	failCreateAcct bool
	nextID         int
}

func newStatefulAuthula() *statefulAuthula {
	return &statefulAuthula{users: map[string]string{}, emails: map[string]string{}}
}

func (a *statefulAuthula) UserByEmail(_ context.Context, email string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.emails[email]
	return ok, nil
}

func (a *statefulAuthula) HashPassword(p string) (string, error) { return "argon2:" + p, nil }

func (a *statefulAuthula) CreateUser(_ context.Context, email string) (AuthulaUser, error) {
	if a.failCreateUser {
		return AuthulaUser{}, errors.New("injected: authula create user")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextID++
	id := fmt.Sprintf("authula-%d", a.nextID)
	a.users[id] = email
	a.emails[email] = id
	return AuthulaUser{ID: id, Email: email}, nil
}

func (a *statefulAuthula) CreateAccount(_ context.Context, _, _, _ string) error {
	if a.failCreateAcct {
		return errors.New("injected: authula create account")
	}
	return nil
}

func (a *statefulAuthula) EnforceFirstLogin(_ context.Context, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enforced = append(a.enforced, userID)
	return nil
}

func (a *statefulAuthula) DeleteUser(_ context.Context, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if email, ok := a.users[userID]; ok {
		delete(a.emails, email)
		delete(a.users, userID)
	}
	return nil
}

func (a *statefulAuthula) liveUsers() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.users)
}

// --- fault-injecting decorators for the aura/telegram legs (real leg except the inject) ---

type faultAuraLeg struct {
	AuraLegWriter
	failCreate bool
	failAudit  bool
}

func (f faultAuraLeg) CreateIdentityWithGrants(ctx context.Context, p AuraLegParams) (string, error) {
	if f.failCreate {
		return "", errors.New("injected: aura leg")
	}
	return f.AuraLegWriter.CreateIdentityWithGrants(ctx, p)
}

func (f faultAuraLeg) WriteAuditRow(ctx context.Context, p AuraLegParams, id string) error {
	if f.failAudit {
		return errors.New("injected: audit row")
	}
	return f.AuraLegWriter.WriteAuditRow(ctx, p, id)
}

type faultTelegram struct {
	TelegramMint
	fail bool
}

func (f faultTelegram) InsertPending(ctx context.Context, token, id string, exp time.Time) error {
	if f.fail {
		return errors.New("injected: telegram mint")
	}
	return f.TelegramMint.InsertPending(ctx, token, id, exp)
}

type faultRecovery struct {
	RecoverySetupWriter
	fail bool
}

func (f faultRecovery) UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error {
	if f.fail {
		return errors.New("injected: recovery write")
	}
	return f.RecoverySetupWriter.UpsertRecovery(ctx, identityID, question, answerHash, answerHashVersion)
}

// --- live test harness ---

type liveSagaEnv struct {
	pool     *pgxpool.Pool
	creator  string
	auraLeg  liveAuraLeg
	telegram liveTelegram
	recovery liveRecovery
}

func newLiveSagaEnv(t *testing.T) *liveSagaEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(ownerCtx(), 90*time.Second)
	t.Cleanup(cancel)
	pool := disposableLiveSagaPool(t, ctx)

	creatorID := uuid.New().String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`,
		creatorID, "onb-creator-"+creatorID[:8]); err != nil {
		t.Fatalf("seed creator identity: %v", err)
	}
	// aura.capability_grants is fail-closed as of migration 0087, so a seed has to say
	// whose grant it is — a bare pool INSERT is refused, which is the point.
	if err := db.WithIdentityTxRaw(ctx, pool, creatorID, func(tx pgx.Tx) error {
		for _, c := range []string{"identity.create", "agent.run"} {
			if _, err := tx.Exec(ctx,
				`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, $2)`,
				creatorID, c); err != nil {
				return fmt.Errorf("grant creator %s: %w", c, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.identities WHERE id=$1`, creatorID)
	})

	return &liveSagaEnv{
		pool: pool, creator: creatorID,
		auraLeg:  liveAuraLeg{pool: pool},
		telegram: liveTelegram{pool: pool},
		recovery: liveRecovery{pool: pool},
	}
}

func disposableLiveSagaPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	dsn := func(role, name string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, name)
	}
	if err := db.EnsureRoles(ctx, dsn("aura", "aura"), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	dbName := "aura_onboarding_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := db.Open(ctx, &db.Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	cleanupNow := true
	defer func() {
		if cleanupNow {
			_, _ = admin.Exec(ownerCtx(), "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
			admin.Close()
		}
	}()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create disposable db: %v", err)
	}
	freshAdmin, err := db.Open(ctx, &db.Config{URL: dsn("aura", dbName)})
	if err != nil {
		t.Fatalf("open disposable admin pool: %v", err)
	}
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+dbName+" TO aura_migrate"); err != nil {
		freshAdmin.Close()
		t.Fatalf("grant create on disposable db: %v", err)
	}
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		freshAdmin.Close()
		t.Fatalf("grant create on public schema: %v", err)
	}
	if _, err := db.Migrate(ctx, dsn("aura_migrate", dbName)); err != nil {
		freshAdmin.Close()
		t.Fatalf("Migrate disposable db: %v", err)
	}
	app, err := db.Open(ctx, &db.Config{URL: dsn("aura_app", dbName)})
	if err != nil {
		freshAdmin.Close()
		t.Fatalf("open disposable app pool: %v", err)
	}
	t.Cleanup(func() {
		app.Close()
		freshAdmin.Close()
		_, _ = admin.Exec(ownerCtx(), "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
		admin.Close()
	})
	cleanupNow = false
	return app
}

// service builds the real onboardingService over the env's real aura/telegram legs + the
// supplied Authula leg, with a started session stamped to the seeded creator.
func (e *liveSagaEnv) service(t *testing.T, au AuthulaCore, leg AuraLegWriter, tg TelegramMint) (*onboardingService, string, string) {
	t.Helper()
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: identity.New(e.pool),
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBotTest",
		Recovery: e.recovery,
		// Isolation ON so the live saga clears the CR-01 provision-time refusal gate.
		MUSRIsolation: true,
	})
	entry := &sessionEntry{creatorIdentityID: e.creator}
	token, err := svc.sessions.put(entry)
	if err != nil {
		t.Fatalf("put session: %v", err)
	}
	email := fmt.Sprintf("onb+%d@aura.local", time.Now().UnixNano())
	return svc, token, email
}

func liveProvReq(email, password string) OnboardingProvisionRequest {
	return OnboardingProvisionRequest{
		Email: email, Password: password,
		SecurityQuestion: "First school?", SecurityAnswer: "Blue School",
		Capabilities: []string{"agent.run"}, LinkTelegram: true,
	}
}

// auraOrphans counts the LIVE aura/telegram/audit rows tied to email (the orphan-critical
// stores). authulaUsers is the in-memory fake's live-user count.
func (e *liveSagaEnv) auraOrphans(t *testing.T, email string) (identities, grants, links, tokens, recovery, audit int) {
	t.Helper()
	ctx := ownerCtx()
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identities WHERE name=$1`, email).Scan(&identities); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	var idID string
	if err := e.pool.QueryRow(ctx, `SELECT id::text FROM aura.identities WHERE name=$1`, email).Scan(&idID); err == nil {
		// The grants count reads inside an identity-scoped tx: aura.capability_grants is
		// fail-closed as of migration 0087, so an unscoped count returns 0 whether or not
		// the saga wrote the row — which would make both the happy-path and the
		// zero-orphan assertions pass vacuously.
		_ = db.WithIdentityTxRaw(ctx, e.pool, idID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM aura.capability_grants WHERE identity_id=$1::uuid`, idID).Scan(&grants)
		})
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_auth_links WHERE identity_id=$1::uuid`, idID).Scan(&links)
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.telegram_setup_pending WHERE identity_id=$1::uuid`, idID).Scan(&tokens)
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_recovery WHERE identity_id=$1::uuid`, idID).Scan(&recovery)
	}
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_audit WHERE new_identity_name=$1`, email).Scan(&audit)
	return
}

// cleanupProvisioned removes the rows a successful provision created for email (identity
// delete cascades grants + links; the immutable audit row is left — append-only + harmless,
// keyed by a unique per-run email).
func (e *liveSagaEnv) cleanupProvisioned(email string) {
	ctx := ownerCtx()
	_, _ = e.pool.Exec(ctx, `DELETE FROM aura.telegram_setup_pending WHERE identity_id IN (SELECT id FROM aura.identities WHERE name=$1)`, email)
	_, _ = e.pool.Exec(ctx, `DELETE FROM aura.identity_recovery WHERE identity_id IN (SELECT id FROM aura.identities WHERE name=$1)`, email)
	_, _ = e.pool.Exec(ctx, `DELETE FROM aura.identities WHERE name=$1`, email)
}

// TestProvisionSagaLive proves the happy path commits all legs + exactly one audit row,
// then each failure-injection point (B1/B2/A/recovery/C) leaves ZERO orphans across EVERY live aura
// store + zero orphan Authula users.
