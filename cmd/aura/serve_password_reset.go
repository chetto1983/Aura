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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

type recoveryStoreAdapter struct {
	pool   *pgxpool.Pool
	pepper []byte
}

const (
	passwordResetChallengeMaxAttempts = 5
	passwordResetTokenMaxAttempts     = 3
)

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
	_, err = sqlc.New(a.pool).InsertPasswordResetChallenge(ctx, passwordResetChallengeParams(identityID, telegramUserID, challenge))
	return err
}

func (a recoveryStoreAdapter) PeekChallenge(ctx context.Context, identityID, code string) error {
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
	if !agui.VerifyOpaqueSecret(code, challenge.CodeHash) {
		if err := q.IncrementPasswordResetChallengeAttempts(ctx, challenge.ID); err != nil {
			return err
		}
		return agui.ErrPasswordResetDenied
	}
	return nil
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
		TokenHash:   agui.HashLookupToken(token, a.pepper),
		ChallengeID: consumed.ID,
		IdentityID:  consumed.IdentityID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		MaxAttempts: passwordResetTokenMaxAttempts,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	committed = true
	return token, nil
}

func (a recoveryStoreAdapter) ResolveResetTokenHash(ctx context.Context, tokenHash string) (string, error) {
	if a.pool == nil {
		return "", errors.New("password reset store unavailable")
	}
	return resolveResetTokenHash(ctx, sqlc.New(a.pool), tokenHash)
}

func (a recoveryStoreAdapter) ClaimResetTokenHash(ctx context.Context, tokenHash string) (agui.ResetTokenClaim, error) {
	if a.pool == nil {
		return nil, errors.New("password reset store unavailable")
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	identityID, err := claimResetTokenHash(ctx, tx, sqlc.New(tx), tokenHash)
	if err != nil {
		if errors.Is(err, agui.ErrPasswordResetDenied) {
			_ = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		return nil, err
	}
	return &pgPasswordResetTokenClaim{
		tx:         tx,
		tokenHash:  tokenHash,
		identityID: identityID,
	}, nil
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

type passwordResetTokenLocker interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func claimResetTokenHash(ctx context.Context, locker passwordResetTokenLocker, q passwordResetTokenQueries, tokenHash string) (string, error) {
	if tokenHash == "" {
		return "", agui.ErrPasswordResetDenied
	}
	token, err := getPasswordResetTokenForUpdate(ctx, locker, tokenHash)
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
	if !token.IdentityID.Valid {
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
	if !consumed.IdentityID.Valid || consumed.IdentityID.Bytes != token.IdentityID.Bytes {
		return "", agui.ErrPasswordResetDenied
	}
	return uuid.UUID(consumed.IdentityID.Bytes).String(), nil
}

func getPasswordResetTokenForUpdate(ctx context.Context, q passwordResetTokenLocker, tokenHash string) (sqlc.AuraPasswordResetTokens, error) {
	row := q.QueryRow(ctx, `
SELECT token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts
FROM aura.password_reset_tokens
WHERE token_hash = $1
FOR UPDATE`, tokenHash)
	var token sqlc.AuraPasswordResetTokens
	err := row.Scan(
		&token.TokenHash,
		&token.ChallengeID,
		&token.IdentityID,
		&token.CreatedAt,
		&token.ExpiresAt,
		&token.ConsumedAt,
		&token.AttemptCount,
		&token.MaxAttempts,
	)
	return token, err
}

func resolveResetTokenHash(ctx context.Context, q passwordResetTokenQueries, tokenHash string) (string, error) {
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
	if !token.IdentityID.Valid {
		return "", agui.ErrPasswordResetDenied
	}
	return uuid.UUID(token.IdentityID.Bytes).String(), nil
}

func consumeResetTokenHash(ctx context.Context, q passwordResetTokenQueries, tokenHash string) (string, error) {
	if _, err := resolveResetTokenHash(ctx, q, tokenHash); err != nil {
		return "", err
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

type pgPasswordResetTokenClaim struct {
	tx         pgx.Tx
	tokenHash  string
	identityID string
	closed     bool
}

func (c *pgPasswordResetTokenClaim) IdentityID() string {
	if c == nil {
		return ""
	}
	return c.identityID
}

func (c *pgPasswordResetTokenClaim) Consume(ctx context.Context) (string, error) {
	if c == nil || c.closed {
		return "", agui.ErrPasswordResetDenied
	}
	if err := c.tx.Commit(ctx); err != nil {
		c.closed = true
		return "", err
	}
	c.closed = true
	return c.identityID, nil
}

func (c *pgPasswordResetTokenClaim) Release(ctx context.Context) error {
	if c == nil || c.closed {
		return nil
	}
	c.closed = true
	return c.tx.Rollback(ctx)
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
	if account.Password != nil && r.core.PasswordService.Verify(password, *account.Password) {
		return agui.ErrPasswordResetSamePassword
	}
	hash, err := r.core.PasswordService.Hash(password)
	if err != nil {
		return errors.New("password reset backend unavailable")
	}
	account.Password = &hash
	if err := r.core.SessionService.DeleteAllByUserID(ctx, authulaUserID); err != nil {
		return errors.New("password reset backend unavailable")
	}
	if _, err := r.core.AccountService.Update(ctx, account); err != nil {
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

func wirePasswordResetService(server passwordResetServer, pool *pgxpool.Pool, deliverer recoveryCodeDeliverer, provider passwordResetCoreProvider, pepper []byte) bool {
	if isNilPasswordResetDependency(server) || pool == nil || isNilPasswordResetDependency(deliverer) || isNilPasswordResetDependency(provider) || len(pepper) == 0 {
		return false
	}
	core := provider.CoreServices()
	if core == nil {
		return false
	}
	server.SetPasswordResetService(agui.NewPasswordResetService(agui.PasswordResetDeps{
		Store:       recoveryStoreAdapter{pool: pool, pepper: append([]byte(nil), pepper...)},
		Messenger:   telegramRecoveryMessenger{deliverer: deliverer},
		Resetter:    authulaPasswordResetter{core: core, pool: pool},
		TokenPepper: pepper,
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

func passwordResetChallengeParams(identityID uuid.UUID, telegramUserID int64, challenge agui.PasswordResetChallenge) sqlc.InsertPasswordResetChallengeParams {
	return sqlc.InsertPasswordResetChallengeParams{
		IdentityID:     pgtype.UUID{Bytes: identityID, Valid: true},
		CodeHash:       challenge.CodeHash,
		TelegramUserID: pgtype.Int8{Int64: telegramUserID, Valid: telegramUserID != 0},
		ExpiresAt:      pgtype.Timestamptz{Time: challenge.ExpiresAt, Valid: true},
		MaxAttempts:    passwordResetChallengeMaxAttempts,
		RequestIpHash:  pgText(challenge.RequestIPHash),
		UserAgentHash:  pgText(challenge.UserAgentHash),
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
