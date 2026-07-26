package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	authulamodels "github.com/Authula/authula/models"
	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/webauth"
)

// serve_onboarding.go wires the composition-root adapters for the Phase-28 onboarding
// wizard + provisioning saga (ONBD-01/02), mirroring serve_governance.go's pattern: the
// agui consumer declares narrow ports (onboarding_provision.go / onboarding_session.go);
// these tiny adapters satisfy them over the daemon's existing seams — the identity Store
// (capability picker + the atomic aura-leg write + compensation + the immutable audit
// row), the Authula provider's CoreServices (Leg B), the Telegram Store (Leg C mint + the
// status poll + pending-token compensation), the recovery adapter (identity_recovery),
// and the profile memory store (the deterministic seed write). The adapters keep the agui
// package free of the telegram import (which would cycle).
//
// buildOnboardingService is best-effort: a seed-only service still answers start and
// provision answers a sanitized backend-unavailable error if Authula, Telegram, recovery
// storage, or bot username are unavailable — never aborting daemon boot (the
// SetGovernanceProviders precedent).

// authulaCoreAdapter satisfies agui.AuthulaCore over Authula's CoreServices, replicating
// the verified DisableSignUp-bypassing create sequence (RESEARCH §Authula): PasswordService
// .Hash → UserService.Create → AccountService.Create, with UserService.Delete for COMP_B.
type authulaCoreAdapter struct {
	core *authulaservices.CoreServices
}

func (a authulaCoreAdapter) UserByEmail(ctx context.Context, email string) (bool, error) {
	u, err := a.core.UserService.GetByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	return u != nil, nil
}

func (a authulaCoreAdapter) HashPassword(password string) (string, error) {
	return a.core.PasswordService.Hash(password)
}

func (a authulaCoreAdapter) CreateUser(ctx context.Context, email string) (agui.AuthulaUser, error) {
	u, err := a.core.UserService.Create(ctx, email, email, true, nil, nil)
	if err != nil {
		return agui.AuthulaUser{}, err
	}
	return agui.AuthulaUser{ID: u.ID, Email: u.Email}, nil
}

func (a authulaCoreAdapter) CreateAccount(ctx context.Context, userID, email, passwordHash string) error {
	_, err := a.core.AccountService.Create(ctx, userID, email, authulamodels.AuthProviderEmail.String(), &passwordHash)
	return err
}

// firstLoginMetaMustChangePassword / firstLoginMetaTOTPRequired are the Authula user-
// metadata markers the D-15 first-login gate reads: the admin-set initial password is
// single-use (force a change) and TOTP must be enrolled before real access. They live in
// the user Metadata jsonb (no schema change, no SMTP). The login-time enforcement that
// reads them is wired at the cutover (plan 12); provisioning SETS them here.
const (
	firstLoginMetaMustChangePassword = "aura_must_change_password"
	firstLoginMetaTOTPRequired       = "aura_totp_enrollment_required"
)

// EnforceFirstLogin sets the D-15 first-login markers on the freshly-created Authula user
// (force password change + TOTP enrollment) via UserService metadata. Idempotent: it
// re-reads the user and overwrites the two boolean markers, so a journaled saga re-run is
// safe. No SMTP, no recovery-link swap (Authula is embedded).
func (a authulaCoreAdapter) EnforceFirstLogin(ctx context.Context, userID string) error {
	u, err := a.core.UserService.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil {
		return fmt.Errorf("enforce first-login: authula user %q not found", userID)
	}
	if u.Metadata == nil {
		u.Metadata = map[string]any{}
	}
	u.Metadata[firstLoginMetaMustChangePassword] = true
	u.Metadata[firstLoginMetaTOTPRequired] = true
	if _, err := a.core.UserService.Update(ctx, u); err != nil {
		return fmt.Errorf("enforce first-login: update authula user: %w", err)
	}
	return nil
}

func (a authulaCoreAdapter) DeleteUser(ctx context.Context, userID string) error {
	return a.core.UserService.Delete(ctx, userID)
}

// auraLegAdapter satisfies agui.AuraLegWriter over Aura's pool + identity Store. Leg A is
// ONE pgx tx (INSERT identity + GrantCapability per cap + LinkOperator), internally
// atomic; the audit row is a tiny FINAL tx after Leg C (RESEARCH L8); compensation deletes
// the identity (grants + link cascade via the migration-0019 FK).
type auraLegAdapter struct {
	pool *pgxpool.Pool
}

// linkOperatorSQL upserts the authula-user → new-identity binding (the same statement
// webauth.IdentityLinker.LinkOperator uses) bound to the Leg-A tx so the link commits
// atomically with the identity + grants.
const linkOperatorSQL = `
	INSERT INTO aura.identity_auth_links (identity_id, authula_user_id)
	VALUES ($1::uuid, $2)
	ON CONFLICT (authula_user_id) DO UPDATE SET identity_id = EXCLUDED.identity_id`

func (a auraLegAdapter) CreateIdentityWithGrants(ctx context.Context, p agui.AuraLegParams) (string, error) {
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
		ID:   pgtype.UUID{Bytes: newID, Valid: true},
		Name: p.IdentityName,
		Kind: "user",
	}); err != nil {
		// A duplicate name (NOT NULL UNIQUE) is a clean 409 (idempotent double-submit →
		// one identity), classified by SQLSTATE 23505 (never a message match).
		if isUniqueViolation(err) {
			return "", agui.ErrOnboardingDuplicate
		}
		return "", fmt.Errorf("create identity: %w", err)
	}
	for _, cap := range p.Capabilities {
		// Backstop the no-escalation re-validation at the store edge. Leg A binds raw sqlc
		// inside this tx, so it must reuse identity.Store's capability grammar helper
		// instead of bypassing Store.GrantCapability.
		if err := identity.ValidateCapabilityName(cap); err != nil {
			return "", agui.ErrOnboardingEscalation
		}
		if err := q.GrantCapability(ctx, sqlc.GrantCapabilityParams{
			IdentityID: pgtype.UUID{Bytes: newID, Valid: true},
			Capability: cap,
		}); err != nil {
			return "", fmt.Errorf("grant capability: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, linkOperatorSQL, newID.String(), p.AuthulaUserID); err != nil {
		return "", fmt.Errorf("link operator: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit aura leg: %w", err)
	}
	committed = true
	return newID.String(), nil
}

func (a auraLegAdapter) WriteAuditRow(ctx context.Context, p agui.AuraLegParams, newIdentityID string) error {
	// A tiny final tx AFTER Leg C succeeds (RESEARCH L8): exactly one immutable audit row
	// per successful create; a rolled-back flow has none. db.WithTx hands the tx-bound
	// Queries that InsertIdentityAuditTx needs.
	return db.WithTx(ctx, a.pool, func(q *sqlc.Queries) error {
		return identity.InsertIdentityAuditTx(ctx, q, identity.IdentityAuditInsert{
			ActorIdentityID:     p.ActorIdentityID,
			NewIdentityID:       newIdentityID,
			NewIdentityName:     p.IdentityName,
			GrantedCapabilities: p.Capabilities,
			AuthulaUserID:       p.AuthulaUserID,
		})
	})
}

func (a auraLegAdapter) DeleteIdentity(ctx context.Context, identityName string) error {
	return identity.New(a.pool).DeleteIdentity(ctx, identityName)
}

// telegramMintAdapter satisfies agui.TelegramMint over the Telegram Store (Leg C mint + the
// status poll), so the agui package never imports telegram (the cycle break).
type telegramMintAdapter struct {
	store *telegram.Store
	pool  *pgxpool.Pool
}

func (a telegramMintAdapter) InsertPending(ctx context.Context, onboardingToken, identityID string, expiresAt time.Time) error {
	return a.store.InsertPending(ctx, telegram.InsertPendingParams{
		OnboardingToken: onboardingToken,
		IdentityID:      identityID,
		GeneratedBy:     "onboarding-wizard",
		ExpiresAt:       expiresAt,
	})
}

func (a telegramMintAdapter) PendingConsumed(ctx context.Context, onboardingToken string) (bool, error) {
	return a.store.PendingConsumed(ctx, onboardingToken)
}

func (a telegramMintAdapter) DeletePending(ctx context.Context, onboardingToken string) error {
	_, err := a.pool.Exec(ctx, `DELETE FROM aura.telegram_setup_pending WHERE onboarding_token=$1`, onboardingToken)
	return err
}

type recoverySetupAdapter struct {
	pool *pgxpool.Pool
}

func (a recoverySetupAdapter) UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error {
	id, err := uuid.Parse(identityID)
	if err != nil {
		return fmt.Errorf("upsert recovery identity: %w", err)
	}
	return sqlc.New(a.pool).UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
		IdentityID:        pgtype.UUID{Bytes: id, Valid: true},
		Question:          question,
		AnswerHash:        answerHash,
		AnswerHashVersion: answerHashVersion,
	})
}

// buildOnboardingService assembles the OnboardingService best-effort. The self-service
// side (capability picker + the deterministic seed write) is always wired; the
// provisioning saga is wired when the composition root supplies an Authula provider.
// The Telegram bot username is resolved live (a best-effort getMe); an empty username
// leaves provisioning unavailable so no identity/recovery/token writes occur.
func buildOnboardingService(ctx context.Context, chat *chatEnv, authulaProvider *webauth.Provider) agui.OnboardingService {
	deps := agui.OnboardingDeps{
		Capabilities: chat.identity,
		Profiles:     newMemoryProfileStore(),
	}
	if chat.pool != nil {
		deps.AuraLeg = auraLegAdapter{pool: chat.pool}
		deps.Telegram = telegramMintAdapter{store: telegram.New(chat.pool), pool: chat.pool}
		deps.Recovery = recoverySetupAdapter{pool: chat.pool}
		// Wire the eager per-identity resource legs (VERIF-3/HI-01): Garage bucket+key +
		// per-identity dirs + saga journal. Nil (pre-cutover / no Garage admin config) →
		// provisionResourceLegs nil-skips each leg, so admin-create still succeeds and just
		// provisions no resources (backward compatible).
		objProv, fsProv, jrnl := buildProvisioningPorts(chat)
		deps.ObjectStore = objProv
		deps.Filesystem = fsProv
		deps.Journal = jrnl
	}
	if authulaProvider != nil {
		if core := authulaProvider.CoreServices(); core != nil {
			deps.Authula = authulaCoreAdapter{core: core}
		}
	}
	deps.BotUsername = resolveBotUsername(ctx, telegram.LoadConfig().BotToken)
	// Live resolver (ONBD Telegram step): a token saved mid-session via Settings yields a
	// working deep-link without a daemon restart. Falls back to deps.BotUsername when empty.
	deps.BotUsernameResolver = newBotUsernameResolver(chat.pool)
	// CR-01/VERIF-5: couple provisioning to the documents-plane isolation flag. When off, the
	// saga REFUSES to create a 2nd identity (agui errIsolationDisabled) so the leak can never
	// be armed by adding a principal; here we also emit a LOUD boot WARN if the daemon already
	// has >1 identity while the flag is off (the documents plane is unscoped right now).
	deps.MUSRIsolation = chat.cfg.MUSRIsolation
	warnIfMultiUserWithoutIsolation(ctx, chat)
	return agui.NewOnboardingService(deps)
}

// warnIfMultiUserWithoutIsolation emits a LOUD boot WARN when more than one live identity
// exists while AURA_MUSR_ISOLATION is off: the documents plane is then UNSCOPED (a 2nd
// identity can read the operator's documents — CR-01). It is best-effort (a ListIdentities
// error is logged, never fatal) and advisory only — the provision-time refusal
// (errIsolationDisabled) + the server_production config-validate gate are the enforcing
// controls; this WARN surfaces an already-multi-user deployment that must run the D-13
// backfill+flip rollout.
func warnIfMultiUserWithoutIsolation(ctx context.Context, chat *chatEnv) {
	if chat == nil || chat.cfg == nil || chat.identity == nil || chat.cfg.MUSRIsolation {
		return
	}
	ids, err := chat.identity.ListIdentities(ctx)
	if err != nil {
		slog.Warn("aura serve: could not count identities for the isolation boot check", "error", err)
		return
	}
	if len(ids) > 1 {
		slog.Warn("aura serve: MULTI-USER WITHOUT ISOLATION — >1 identity exists and AURA_MUSR_ISOLATION is off; the documents plane is UNSCOPED (a 2nd identity can read the operator's documents). Run `aura documents backfill` then set AURA_MUSR_ISOLATION=true — see docs/runbooks/musr-rollout.md",
			"identity_count", len(ids))
	}
}

type onboardingStatusAdapter struct {
	store *memoryProfileStore
}

func newOnboardingStatusAdapter(_ *chatEnv) onboardingStatusAdapter {
	return onboardingStatusAdapter{store: newMemoryProfileStore()}
}

// OnboardingStatus reads the onboarding sentinel facts from the memory graph. It fails
// OPEN to Required (re-prompt) when the memory sidecar is unreachable — matching the
// previous not-found behavior, never blocking the cockpit on a memory outage.
func (a onboardingStatusAdapter) OnboardingStatus(ctx context.Context, identityID string) (agui.OnboardingStatus, error) {
	if a.store == nil || identityID == "" {
		return agui.OnboardingStatus{Required: true}, nil
	}
	st, err := a.store.Status(ctx, identityID)
	if err != nil {
		slog.Warn("onboarding status: memory read failed, defaulting to required", "id", identityID, "err", err)
		return agui.OnboardingStatus{Required: true}, nil
	}
	return agui.OnboardingStatus{
		Required:  !st.Completed && !st.Skipped,
		Completed: st.Completed,
		Skipped:   st.Skipped,
	}, nil
}

// resolveBotUsername does a best-effort live getMe to learn the bot username for the
// onboarding deep-link (t.me/<bot>?start=<token>). It NEVER logs the token (the
// telegramGetMeProbe precedent, T-13-07). An empty token or a failed probe yields ""; the
// core service treats that as provisioning unavailable and fails before writes.
func resolveBotUsername(ctx context.Context, token string) string {
	if token == "" {
		return ""
	}
	username, err := telegramGetMeProbe(ctx, token)
	if err != nil {
		return ""
	}
	return username
}

// isUniqueViolation classifies a pgx error as SQLSTATE 23505 via errors.As + pgErr.Code
// (never a message match — mirrors internal/identity.isUniqueViolation).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
