package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	authulamodels "github.com/Authula/authula/models"
	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
// status poll + pending-token compensation), the recovery adapter (identity_recovery),
// and the LLM extractor + profile store (the interview). The adapters keep the agui
// package free of the telegram import (which would cycle).
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

type recoveryStoreAdapter struct {
	pool *pgxpool.Pool
}

func (a recoveryStoreAdapter) LookupByEmail(ctx context.Context, email string) (agui.RecoveryRecord, error) {
	if a.pool == nil {
		return agui.RecoveryRecord{}, errors.New("password reset store unavailable")
	}
	row, err := sqlc.New(a.pool).LookupRecoveryByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agui.RecoveryRecord{}, agui.ErrPasswordResetDenied
		}
		return agui.RecoveryRecord{}, err
	}
	if !row.IdentityID.Valid || row.AuthulaUserID == "" || row.AnswerHash == "" ||
		row.AnswerHashVersion == "" || row.TelegramUserID == 0 {
		return agui.RecoveryRecord{}, agui.ErrPasswordResetDenied
	}
	return agui.RecoveryRecord{
		IdentityID:        uuid.UUID(row.IdentityID.Bytes).String(),
		Email:             row.Email,
		AuthulaUserID:     row.AuthulaUserID,
		Question:          row.Question,
		AnswerHash:        row.AnswerHash,
		AnswerHashVersion: row.AnswerHashVersion,
		TelegramUserID:    row.TelegramUserID,
	}, nil
}

func (a recoveryStoreAdapter) StartChallenge(ctx context.Context, challenge agui.PasswordResetChallenge) error {
	if a.pool == nil {
		return errors.New("password reset store unavailable")
	}
	identityID, err := parsePasswordResetIdentityID(challenge.IdentityID)
	if err != nil {
		return agui.ErrPasswordResetDenied
	}
	telegramUserID, err := lookupPasswordResetTelegramUserID(ctx, a.pool, identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agui.ErrPasswordResetDenied
		}
		return err
	}
	_, err = sqlc.New(a.pool).InsertPasswordResetChallenge(ctx, sqlc.InsertPasswordResetChallengeParams{
		IdentityID:     pgtype.UUID{Bytes: identityID, Valid: true},
		CodeHash:       challenge.CodeHash,
		TelegramUserID: pgtype.Int8{Int64: telegramUserID, Valid: telegramUserID != 0},
		ExpiresAt:      pgtype.Timestamptz{Time: challenge.ExpiresAt, Valid: true},
		MaxAttempts:    3,
		RequestIpHash:  pgText(challenge.RequestIPHash),
		UserAgentHash:  pgText(challenge.UserAgentHash),
	})
	return err
}

func (a recoveryStoreAdapter) VerifyChallenge(ctx context.Context, identityID, code string) (string, error) {
	if a.pool == nil {
		return "", errors.New("password reset store unavailable")
	}
	id, err := parsePasswordResetIdentityID(identityID)
	if err != nil {
		return "", agui.ErrPasswordResetDenied
	}
	q := sqlc.New(a.pool)
	challenge, err := q.GetActivePasswordResetChallenge(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", agui.ErrPasswordResetDenied
		}
		return "", err
	}
	if !agui.VerifyOpaqueSecret(code, challenge.CodeHash) {
		if err := q.IncrementPasswordResetChallengeAttempts(ctx, challenge.ID); err != nil {
			return "", err
		}
		return "", agui.ErrPasswordResetDenied
	}

	token, err := newPasswordResetToken()
	if err != nil {
		return "", err
	}
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
	tq := sqlc.New(tx)
	consumed, err := tq.ConsumePasswordResetChallenge(ctx, challenge.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", agui.ErrPasswordResetDenied
		}
		return "", err
	}
	if _, err := tq.InsertPasswordResetToken(ctx, sqlc.InsertPasswordResetTokenParams{
		TokenHash:   agui.HashLookupToken(token),
		ChallengeID: consumed.ID,
		IdentityID:  consumed.IdentityID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		MaxAttempts: 3,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	committed = true
	return token, nil
}

func (a recoveryStoreAdapter) ConsumeResetTokenHash(ctx context.Context, tokenHash string) (string, error) {
	if a.pool == nil {
		return "", errors.New("password reset store unavailable")
	}
	return consumeResetTokenHash(ctx, sqlc.New(a.pool), tokenHash)
}

func (a recoveryStoreAdapter) RecordChallengeAttempt(ctx context.Context, identityID string) error {
	if a.pool == nil {
		return errors.New("password reset store unavailable")
	}
	id, err := parsePasswordResetIdentityID(identityID)
	if err != nil {
		return agui.ErrPasswordResetDenied
	}
	q := sqlc.New(a.pool)
	challenge, err := q.GetActivePasswordResetChallenge(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agui.ErrPasswordResetDenied
		}
		return err
	}
	return q.IncrementPasswordResetChallengeAttempts(ctx, challenge.ID)
}

func (a recoveryStoreAdapter) RecordRecoveryEvent(ctx context.Context, event agui.RecoveryEvent) error {
	if a.pool == nil {
		return errors.New("password reset store unavailable")
	}
	identityID := pgtype.UUID{}
	if event.IdentityID != "" {
		id, err := parsePasswordResetIdentityID(event.IdentityID)
		if err != nil {
			return err
		}
		identityID = pgtype.UUID{Bytes: id, Valid: true}
	}
	metadata := []byte(`{}`)
	if event.EmailHash != "" {
		var err error
		metadata, err = json.Marshal(map[string]string{"email_hash": event.EmailHash})
		if err != nil {
			return err
		}
	}
	_, err := sqlc.New(a.pool).InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
		IdentityID:    identityID,
		Event:         event.Event,
		RequestIpHash: pgText(event.RequestIPHash),
		UserAgentHash: pgText(event.UserAgentHash),
		Metadata:      metadata,
	})
	return err
}

type passwordResetTokenQueries interface {
	GetPasswordResetToken(ctx context.Context, tokenHash string) (sqlc.AuraPasswordResetTokens, error)
	ConsumePasswordResetToken(ctx context.Context, tokenHash string) (sqlc.AuraPasswordResetTokens, error)
	IncrementPasswordResetTokenAttempts(ctx context.Context, tokenHash string) error
}

func consumeResetTokenHash(ctx context.Context, q passwordResetTokenQueries, tokenHash string) (string, error) {
	if q == nil || tokenHash == "" {
		return "", agui.ErrPasswordResetDenied
	}
	token, err := q.GetPasswordResetToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = q.IncrementPasswordResetTokenAttempts(ctx, tokenHash)
			return "", agui.ErrPasswordResetDenied
		}
		return "", err
	}
	if token.ConsumedAt.Valid || !token.ExpiresAt.Valid || !token.ExpiresAt.Time.After(time.Now()) ||
		token.AttemptCount >= token.MaxAttempts {
		_ = q.IncrementPasswordResetTokenAttempts(ctx, tokenHash)
		return "", agui.ErrPasswordResetDenied
	}
	consumed, err := q.ConsumePasswordResetToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = q.IncrementPasswordResetTokenAttempts(ctx, tokenHash)
			return "", agui.ErrPasswordResetDenied
		}
		return "", err
	}
	if !consumed.IdentityID.Valid {
		return "", agui.ErrPasswordResetDenied
	}
	return uuid.UUID(consumed.IdentityID.Bytes).String(), nil
}

type recoveryCodeDeliverer interface {
	DeliverToIdentity(ctx context.Context, identityID, text string) (bool, error)
}

type telegramRecoveryMessenger struct {
	deliverer recoveryCodeDeliverer
}

func (m telegramRecoveryMessenger) SendRecoveryCode(ctx context.Context, identityID, code string) error {
	if m.deliverer == nil {
		return errors.New("recovery delivery unavailable")
	}
	delivered, err := m.deliverer.DeliverToIdentity(ctx, identityID, recoveryCodeMessage(code))
	if err != nil {
		return errors.New("recovery delivery unavailable")
	}
	if !delivered {
		return errors.New("recovery delivery unavailable")
	}
	return nil
}

func recoveryCodeMessage(code string) string {
	return fmt.Sprintf("Aura recovery code: %s\nThis code expires in 10 minutes. If you did not request this, you can ignore this message.", code)
}

type authulaPasswordResetter struct {
	core *authulaservices.CoreServices
	pool *pgxpool.Pool
}

func (r authulaPasswordResetter) SetPassword(ctx context.Context, identityID, password string) error {
	if r.core == nil || r.core.PasswordService == nil || r.core.AccountService == nil ||
		r.core.SessionService == nil || r.pool == nil {
		return errors.New("password reset backend unavailable")
	}
	id, err := parsePasswordResetIdentityID(identityID)
	if err != nil {
		return errors.New("password reset backend unavailable")
	}
	var authulaUserID string
	if err := r.pool.QueryRow(ctx,
		`SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id=$1`,
		pgtype.UUID{Bytes: id, Valid: true},
	).Scan(&authulaUserID); err != nil {
		return errors.New("password reset backend unavailable")
	}
	account, err := r.core.AccountService.GetByUserIDAndProvider(ctx, authulaUserID, authulamodels.AuthProviderEmail.String())
	if err != nil || account == nil {
		return errors.New("password reset backend unavailable")
	}
	hash, err := r.core.PasswordService.Hash(password)
	if err != nil {
		return errors.New("password reset backend unavailable")
	}
	account.Password = &hash
	if _, err := r.core.AccountService.Update(ctx, account); err != nil {
		return errors.New("password reset backend unavailable")
	}
	if err := r.core.SessionService.DeleteAllByUserID(ctx, authulaUserID); err != nil {
		return errors.New("password reset backend unavailable")
	}
	return nil
}

type passwordResetServer interface {
	SetPasswordResetService(*agui.PasswordResetService)
}

type passwordResetCoreProvider interface {
	CoreServices() *authulaservices.CoreServices
}

func wirePasswordResetService(server passwordResetServer, pool *pgxpool.Pool, deliverer recoveryCodeDeliverer, provider passwordResetCoreProvider) bool {
	if isNilPasswordResetDependency(server) || pool == nil || isNilPasswordResetDependency(deliverer) || isNilPasswordResetDependency(provider) {
		return false
	}
	core := provider.CoreServices()
	if core == nil {
		return false
	}
	server.SetPasswordResetService(agui.NewPasswordResetService(agui.PasswordResetDeps{
		Store:     recoveryStoreAdapter{pool: pool},
		Messenger: telegramRecoveryMessenger{deliverer: deliverer},
		Resetter:  authulaPasswordResetter{core: core, pool: pool},
	}))
	return true
}

func isNilPasswordResetDependency(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func lookupPasswordResetTelegramUserID(ctx context.Context, pool *pgxpool.Pool, identityID uuid.UUID) (int64, error) {
	var telegramUserID int64
	err := pool.QueryRow(ctx, `
SELECT telegram_user_id
FROM aura.telegram_accounts
WHERE identity_id = $1
ORDER BY COALESCE(last_seen_at, added_at) DESC, added_at DESC, telegram_user_id DESC
LIMIT 1`, pgtype.UUID{Bytes: identityID, Valid: true}).Scan(&telegramUserID)
	return telegramUserID, err
}

func parsePasswordResetIdentityID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, err
	}
	return id, nil
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func newPasswordResetToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// buildOnboardingService assembles the OnboardingService best-effort. The interview side
// (capability picker + LLM extraction + profile write) is always wired when a pool exists;
// the provisioning saga is wired when the composition root supplies an Authula provider
// (either the active auth provider or a provisioning-only provider while passphrase login
// remains active). The Telegram bot username is resolved live (a best-effort getMe); an
// empty username leaves provisioning unavailable so no identity/recovery/token writes occur.
func buildOnboardingService(ctx context.Context, chat *chatEnv, authulaProvider *webauth.Provider) agui.OnboardingService {
	deps := agui.OnboardingDeps{
		Capabilities: chat.identity,
		Extractor:    onboarding.NewLLMAnswerExtractor(chat.client, chat.cfg.LLM.Model),
		Profiles:     profile.NewStore(""),
	}
	if chat.pool != nil {
		deps.AuraLeg = auraLegAdapter{pool: chat.pool}
		deps.Telegram = telegramMintAdapter{store: telegram.New(chat.pool), pool: chat.pool}
		deps.Recovery = recoverySetupAdapter{pool: chat.pool}
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
