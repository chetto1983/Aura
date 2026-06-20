//go:build db_integration

// Live cross-store provisioning saga coverage (ONBD-01a/01b — the high-risk track). It
// drives the REAL onboardingService.Provision against a running aura-postgres — the aura.*
// pool, the immutable aura.identity_audit table, and the aura.telegram_setup_pending table
// — asserting the saga's all-or-nothing guarantee against the LIVE orphan-critical stores:
// every RESEARCH failure-injection point (B1/B2/A/C) leaves ZERO orphans in EVERY store, a
// double-submit yields exactly one identity (clean 409 via the NOT NULL UNIQUE name),
// exactly one IMMUTABLE audit row per success (none for a rolled-back flow, and UPDATE/
// DELETE rejected), and no secret reaches a log line over a full run.
//
// Authula leg note: the agui package runs under goleak.VerifyTestMain (main_test.go), and
// the embedded Authula provider spawns long-lived database/sql + rate-limit cleanup
// goroutines that goleak (correctly) flags. Constructing the real provider here would fail
// the package goleak gate. So Leg B uses a STATEFUL Authula fake that records created/
// deleted users in-memory (so COMP_B's zero-orphan-user property is provable) with the
// SAME ordered semantics as the real adapter. The REAL Authula CoreServices Leg B
// (PasswordService.Hash + UserService.Create/Delete + AccountService.Create) is live-proven
// in internal/webauth/authula_integration_test.go + authula_multiuser_test.go; the
// composition-root adapter that wires it is cmd/aura/serve_onboarding.go.
//
// Run via (stack up, password from the container/.env):
//
//	go test -tags db_integration ./internal/agui -run 'TestProvisionSagaLive|TestProvisionIdempotent|TestIdentityAuditImmutable|TestProvisionNoSecretInLogsLive' -count=1 -p 1
//
// Requires AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD (migratedPool). Run with
// -p 1 — the shared Postgres is also exercised by a parallel session. No-skip-as-green:
// migratedPool's envOrSkip t.Fatals under $CI when the DSN is unset.

package agui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/onboarding"
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
	for _, c := range p.Capabilities {
		if c == "*" {
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

// statefulAuthula is the in-memory Authula leg (goleak-clean — no provider goroutines). It
// records created/deleted users so COMP_B's zero-orphan-user property is provable, with
// optional fault injection at CreateUser (B1) / CreateAccount (B2). It mirrors the real
// adapter's ordered semantics (Hash → CreateUser → CreateAccount; DeleteUser cascades the
// account). The real CoreServices Leg B is proven in the webauth integration tests.
type statefulAuthula struct {
	mu             sync.Mutex
	users          map[string]string // userID -> email
	emails         map[string]string // email -> userID
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
	fail bool
}

func (f faultAuraLeg) CreateIdentityWithGrants(ctx context.Context, p AuraLegParams) (string, error) {
	if f.fail {
		return "", errors.New("injected: aura leg")
	}
	return f.AuraLegWriter.CreateIdentityWithGrants(ctx, p)
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

// --- live test harness ---

type liveSagaEnv struct {
	pool     *pgxpool.Pool
	creator  string
	auraLeg  liveAuraLeg
	telegram liveTelegram
}

func newLiveSagaEnv(t *testing.T) *liveSagaEnv {
	t.Helper()
	pool := migratedPool(t)
	ctx := context.Background()

	creatorID := uuid.New().String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`,
		creatorID, "onb-creator-"+creatorID[:8]); err != nil {
		t.Fatalf("seed creator identity: %v", err)
	}
	for _, c := range []string{"identity.create", "agent.run"} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, $2)`,
			creatorID, c); err != nil {
			t.Fatalf("grant creator %s: %v", c, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, creatorID)
	})

	return &liveSagaEnv{
		pool: pool, creator: creatorID,
		auraLeg:  liveAuraLeg{pool: pool},
		telegram: liveTelegram{pool: pool},
	}
}

// service builds the real onboardingService over the env's real aura/telegram legs + the
// supplied Authula leg, with a started session stamped to the seeded creator.
func (e *liveSagaEnv) service(t *testing.T, au AuthulaCore, leg AuraLegWriter, tg TelegramMint) (*onboardingService, string, string) {
	t.Helper()
	svc := newOnboardingService(OnboardingDeps{
		Capabilities: identity.New(e.pool),
		Extractor:    &countingExtractor{},
		Profiles:     &recordingProfileWriter{},
		Authula:      au, AuraLeg: leg, Telegram: tg, BotUsername: "AuraBotTest",
	})
	entry := &sessionEntry{session: onboarding.NewSession("", "new-identity"), creatorIdentityID: e.creator}
	token, err := svc.sessions.put(entry)
	if err != nil {
		t.Fatalf("put session: %v", err)
	}
	email := fmt.Sprintf("onb+%d@aura.local", time.Now().UnixNano())
	return svc, token, email
}

// auraOrphans counts the LIVE aura/telegram/audit rows tied to email (the orphan-critical
// stores). authulaUsers is the in-memory fake's live-user count.
func (e *liveSagaEnv) auraOrphans(t *testing.T, email string) (identities, grants, links, tokens, audit int) {
	t.Helper()
	ctx := context.Background()
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identities WHERE name=$1`, email).Scan(&identities); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	var idID string
	if err := e.pool.QueryRow(ctx, `SELECT id::text FROM aura.identities WHERE name=$1`, email).Scan(&idID); err == nil {
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.capability_grants WHERE identity_id=$1::uuid`, idID).Scan(&grants)
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_auth_links WHERE identity_id=$1::uuid`, idID).Scan(&links)
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.telegram_setup_pending WHERE identity_id=$1::uuid`, idID).Scan(&tokens)
	}
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_audit WHERE new_identity_name=$1`, email).Scan(&audit)
	return
}

// cleanupProvisioned removes the rows a successful provision created for email (identity
// delete cascades grants + links; the immutable audit row is left — append-only + harmless,
// keyed by a unique per-run email).
func (e *liveSagaEnv) cleanupProvisioned(email string) {
	ctx := context.Background()
	_, _ = e.pool.Exec(ctx, `DELETE FROM aura.telegram_setup_pending WHERE identity_id IN (SELECT id FROM aura.identities WHERE name=$1)`, email)
	_, _ = e.pool.Exec(ctx, `DELETE FROM aura.identities WHERE name=$1`, email)
}

// TestProvisionSagaLive proves the happy path commits all legs + exactly one audit row,
// then each failure-injection point (B1/B2/A/C) leaves ZERO orphans across EVERY live aura
// store + zero orphan Authula users.
func TestProvisionSagaLive(t *testing.T) {
	env := newLiveSagaEnv(t)

	t.Run("happy path commits all legs + one audit row", func(t *testing.T) {
		au := newStatefulAuthula()
		svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
		t.Cleanup(func() { env.cleanupProvisioned(email) })
		resp, err := svc.Provision(context.Background(), tok, OnboardingProvisionRequest{
			Email: email, Password: "temp-pw-123", Capabilities: []string{"agent.run"}, LinkTelegram: true,
		})
		if err != nil {
			t.Fatalf("Provision: %v", err)
		}
		ids, grants, links, tokens, audit := env.auraOrphans(t, email)
		if ids != 1 || grants != 1 || links != 1 || tokens != 1 || audit != 1 || au.liveUsers() != 1 {
			t.Fatalf("happy path stores: identities=%d grants=%d links=%d tokens=%d audit=%d authula=%d",
				ids, grants, links, tokens, audit, au.liveUsers())
		}
		if resp.DeepLink == "" || resp.QRSVG == "" {
			t.Error("happy path must return deep-link + QR")
		}
		if strings.Contains(resp.QRSVG, "temp-pw-123") {
			t.Error("QR SVG leaked the password")
		}
	})

	inject := []struct {
		name  string
		auFn  func() *statefulAuthula
		legFn func() AuraLegWriter
		tgFn  func() TelegramMint
	}{
		{"B1 create-user fails", func() *statefulAuthula { a := newStatefulAuthula(); a.failCreateUser = true; return a },
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return env.telegram }},
		{"B2 create-account fails", func() *statefulAuthula { a := newStatefulAuthula(); a.failCreateAcct = true; return a },
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return env.telegram }},
		{"A aura-leg fails", newStatefulAuthula,
			func() AuraLegWriter { return faultAuraLeg{AuraLegWriter: env.auraLeg, fail: true} }, func() TelegramMint { return env.telegram }},
		{"C telegram mint fails", newStatefulAuthula,
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return faultTelegram{TelegramMint: env.telegram, fail: true} }},
	}
	for _, tc := range inject {
		t.Run(tc.name+" -> zero orphans", func(t *testing.T) {
			au := tc.auFn()
			svc, tok, email := env.service(t, au, tc.legFn(), tc.tgFn())
			t.Cleanup(func() { env.cleanupProvisioned(email) })
			if _, err := svc.Provision(context.Background(), tok, OnboardingProvisionRequest{
				Email: email, Password: "temp-pw-123", Capabilities: []string{"agent.run"}, LinkTelegram: true,
			}); err == nil {
				t.Fatalf("%s: provision must error", tc.name)
			}
			ids, grants, links, tokens, audit := env.auraOrphans(t, email)
			if ids != 0 || grants != 0 || links != 0 || tokens != 0 || audit != 0 || au.liveUsers() != 0 {
				t.Fatalf("%s LEFT ORPHANS: identities=%d grants=%d links=%d tokens=%d audit=%d authula=%d",
					tc.name, ids, grants, links, tokens, audit, au.liveUsers())
			}
		})
	}
}

// TestProvisionIdempotent proves a double-submit (same email) yields exactly one identity +
// a clean ErrOnboardingDuplicate (the aura.identities NOT NULL UNIQUE name 23505), never a
// 2nd identity, and the 2nd attempt leaves no orphan Authula user.
func TestProvisionIdempotent(t *testing.T) {
	env := newLiveSagaEnv(t)
	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })

	if _, err := svc.Provision(context.Background(), tok, OnboardingProvisionRequest{
		Email: email, Password: "temp-pw-123", Capabilities: []string{"agent.run"}, LinkTelegram: false,
	}); err != nil {
		t.Fatalf("first provision: %v", err)
	}

	// Second provision with the SAME email on a fresh session: the Authula pre-check sees
	// the existing user → ErrOnboardingDuplicate (no write); one identity remains.
	svc2, tok2, _ := env.service(t, au, env.auraLeg, env.telegram)
	_, err := svc2.Provision(context.Background(), tok2, OnboardingProvisionRequest{
		Email: email, Password: "temp-pw-123", Capabilities: []string{"agent.run"}, LinkTelegram: false,
	})
	if !errors.Is(err, ErrOnboardingDuplicate) {
		t.Fatalf("double-submit err = %v, want ErrOnboardingDuplicate", err)
	}
	var count int
	if err := env.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM aura.identities WHERE name=$1`, email).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("double-submit produced %d identities, want exactly 1", count)
	}
	if au.liveUsers() != 1 {
		t.Fatalf("double-submit left %d Authula users, want 1 (no orphan from the 2nd attempt)", au.liveUsers())
	}
}

// TestIdentityAuditImmutable proves exactly one immutable audit row on success and that it
// cannot be mutated (UPDATE/DELETE rejected by the append-only trigger, T-28-05-06). A
// rolled-back flow (B1) writes none.
func TestIdentityAuditImmutable(t *testing.T) {
	env := newLiveSagaEnv(t)

	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })
	if _, err := svc.Provision(context.Background(), tok, OnboardingProvisionRequest{
		Email: email, Password: "temp-pw-123", Capabilities: []string{"agent.run"}, LinkTelegram: false,
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	ctx := context.Background()
	var auditID string
	if err := env.pool.QueryRow(ctx,
		`SELECT id::text FROM aura.identity_audit WHERE new_identity_name=$1`, email).Scan(&auditID); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE aura.identity_audit SET new_identity_name='x' WHERE id=$1`, auditID); err == nil {
		t.Error("UPDATE on identity_audit succeeded — must be append-only")
	}
	if _, err := env.pool.Exec(ctx, `DELETE FROM aura.identity_audit WHERE id=$1`, auditID); err == nil {
		t.Error("DELETE on identity_audit succeeded — must be append-only")
	}

	// A rolled-back flow (B1) writes no audit row.
	au2 := newStatefulAuthula()
	au2.failCreateUser = true
	svc2, tok2, email2 := env.service(t, au2, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email2) })
	_, _ = svc2.Provision(ctx, tok2, OnboardingProvisionRequest{
		Email: email2, Password: "temp-pw-123", Capabilities: []string{"agent.run"}, LinkTelegram: false,
	})
	var rolledBackAudit int
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_audit WHERE new_identity_name=$1`, email2).Scan(&rolledBackAudit)
	if rolledBackAudit != 0 {
		t.Fatalf("rolled-back flow wrote %d audit rows, want 0", rolledBackAudit)
	}
}

// TestProvisionNoSecretInLogsLive captures slog over a full LIVE provision run and asserts
// the Authula password never appears in any log line — the cross-cutting no-leak guarantee
// exercised against the real aura stores.
func TestProvisionNoSecretInLogsLive(t *testing.T) {
	env := newLiveSagaEnv(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const secret = "L1ve-Sup3r-Secret-PW!"
	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })
	if _, err := svc.Provision(context.Background(), tok, OnboardingProvisionRequest{
		Email: email, Password: secret, Capabilities: []string{"agent.run"}, LinkTelegram: true,
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("Authula password leaked into a log line over a live run:\n%s", buf.String())
	}
}
