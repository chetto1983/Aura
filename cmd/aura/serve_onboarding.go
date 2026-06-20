package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/profile"
	"github.com/chetto1983/aura/internal/webauth"
)

// serve_onboarding.go wires the composition-root adapters for the Phase-28 onboarding
// wizard + provisioning saga (ONBD-01/02), mirroring serve_governance.go's pattern: the
// agui consumer declares narrow ports (onboarding_provision.go / onboarding_session.go);
// these tiny adapters satisfy them over the daemon's existing seams — the identity Store
// (capability picker + the atomic aura-leg write + compensation + the immutable audit
// row), the Authula provider's CoreServices (Leg B), the Telegram Store (Leg C mint + the
// status poll), and the LLM extractor + profile store (the interview). The adapters keep
// the agui package free of the telegram import (which would cycle).
//
// buildOnboardingService is best-effort: provisioning is wired only when Authula is the
// auth provider (the passphrase path leaves the provider nil), so an interview-only
// service still answers start/step and provision answers a sanitized backend-unavailable
// error — never aborting daemon boot (the SetGovernanceProviders precedent).

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
		// Backstop the no-escalation re-validation at the store edge: never grant '*' (the
		// raw sqlc GrantCapability does not reject it; identity.Store.GrantCapability does,
		// but Leg A binds the tx-level sqlc query, so the check lands here too).
		if cap == "*" {
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

// buildOnboardingService assembles the OnboardingService best-effort. The interview side
// (capability picker + LLM extraction + profile write) is always wired when a pool exists;
// the provisioning saga is wired only when Authula is the auth provider (provider !=
// nil) — the passphrase path cannot mint an Authula login, so provision degrades to a
// sanitized backend-unavailable error. The Telegram bot username is resolved live (a
// best-effort getMe); an empty username simply omits the deep-link/QR (the identity is
// still created + the token minted).
func buildOnboardingService(ctx context.Context, chat *chatEnv, authulaProvider *webauth.Provider) agui.OnboardingService {
	deps := agui.OnboardingDeps{
		Capabilities: chat.identity,
		Extractor:    onboarding.NewLLMAnswerExtractor(chat.client, chat.cfg.LLM.Model),
		Profiles:     profile.NewStore(""),
	}
	if chat.pool != nil {
		deps.AuraLeg = auraLegAdapter{pool: chat.pool}
		deps.Telegram = telegramMintAdapter{store: telegram.New(chat.pool)}
	}
	if authulaProvider != nil {
		if core := authulaProvider.CoreServices(); core != nil {
			deps.Authula = authulaCoreAdapter{core: core}
		}
	}
	deps.BotUsername = resolveBotUsername(ctx, telegram.LoadConfig().BotToken)
	return agui.NewOnboardingService(deps)
}

// resolveBotUsername does a best-effort live getMe to learn the bot username for the
// onboarding deep-link (t.me/<bot>?start=<token>). It NEVER logs the token (the
// telegramGetMeProbe precedent, T-13-07). An empty token or a failed probe yields "" so
// the wizard omits the deep-link/QR rather than crashing boot.
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
