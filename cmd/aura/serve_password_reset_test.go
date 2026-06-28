package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestRecoveryCodeMessageIsNeutralAndContainsCode(t *testing.T) {
	msg := recoveryCodeMessage("ABC12345")

	for _, want := range []string{"ABC12345", "Aura", "10"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	for _, secret := range []string{"reset@example.com", "Davide", "identity-1", "password", "answer"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(secret)) {
			t.Fatalf("message %q leaked %q", msg, secret)
		}
	}
}

func TestAuthulaPasswordResetterRejectsMissingCore(t *testing.T) {
	err := (authulaPasswordResetter{}).SetPassword(context.Background(), "identity-1", "pw")
	if err == nil {
		t.Fatal("SetPassword with missing dependencies succeeded, want fail-closed error")
	}
	if strings.Contains(err.Error(), "pw") {
		t.Fatalf("SetPassword error leaked raw password: %v", err)
	}
}

func TestConsumeResetTokenHashUsesAlreadyHashedToken(t *testing.T) {
	identityID := uuid.New()
	challengeID := uuid.New()
	tokenHash := agui.HashLookupToken("raw-reset-token")
	queries := &fakePasswordResetTokenQueries{
		wantTokenHash: tokenHash,
		token: sqlc.AuraPasswordResetTokens{
			TokenHash:   tokenHash,
			ChallengeID: pgtype.UUID{Bytes: challengeID, Valid: true},
			IdentityID:  pgtype.UUID{Bytes: identityID, Valid: true},
			ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
			MaxAttempts: 3,
		},
	}

	got, err := consumeResetTokenHash(context.Background(), queries, tokenHash)
	if err != nil {
		t.Fatalf("consumeResetTokenHash: %v", err)
	}
	if got != identityID.String() {
		t.Fatalf("identityID = %q, want %q", got, identityID.String())
	}
	if queries.incremented {
		t.Fatal("valid token path incremented attempts")
	}
}

func TestConsumeResetTokenHashIncrementsAttemptsOnInvalidToken(t *testing.T) {
	tokenHash := agui.HashLookupToken("raw-reset-token")
	queries := &fakePasswordResetTokenQueries{
		wantTokenHash: tokenHash,
		getErr:        pgx.ErrNoRows,
	}

	_, err := consumeResetTokenHash(context.Background(), queries, tokenHash)
	if !errors.Is(err, agui.ErrPasswordResetDenied) {
		t.Fatalf("consumeResetTokenHash err = %v, want ErrPasswordResetDenied", err)
	}
	if !queries.incremented {
		t.Fatal("invalid token did not increment attempts by the supplied hash")
	}
}

func TestWirePasswordResetServiceRequiresAllDependencies(t *testing.T) {
	server := &fakePasswordResetServer{}
	deliverer := &fakeRecoveryDeliverer{}
	provider := fakePasswordResetCoreProvider{core: &authulaservices.CoreServices{}}
	pool := &pgxpool.Pool{}

	if !wirePasswordResetService(server, pool, deliverer, provider) {
		t.Fatal("wirePasswordResetService returned false with pool, deliverer, and Authula provider")
	}
	if server.service == nil {
		t.Fatal("SetPasswordResetService was not called")
	}

	for _, tc := range []struct {
		name      string
		pool      *pgxpool.Pool
		deliverer recoveryCodeDeliverer
		provider  passwordResetCoreProvider
	}{
		{name: "missing pool", deliverer: deliverer, provider: provider},
		{name: "missing deliverer", pool: pool, provider: provider},
		{name: "missing provider", pool: pool, deliverer: deliverer},
		{name: "missing core", pool: pool, deliverer: deliverer, provider: fakePasswordResetCoreProvider{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &fakePasswordResetServer{}
			if wirePasswordResetService(server, tc.pool, tc.deliverer, tc.provider) {
				t.Fatal("wirePasswordResetService returned true for incomplete dependencies")
			}
			if server.service != nil {
				t.Fatal("SetPasswordResetService was called for incomplete dependencies")
			}
		})
	}
}

func TestWirePasswordResetServiceTreatsTypedNilProviderAsMissing(t *testing.T) {
	server := &fakePasswordResetServer{}
	deliverer := &fakeRecoveryDeliverer{}
	pool := &pgxpool.Pool{}
	var provider *panicPasswordResetCoreProvider

	if wirePasswordResetService(server, pool, deliverer, provider) {
		t.Fatal("wirePasswordResetService returned true for typed nil provider")
	}
	if server.service != nil {
		t.Fatal("SetPasswordResetService was called for typed nil provider")
	}
}

func TestPasswordResetAdaptersSatisfyPorts(t *testing.T) {
	var _ agui.PasswordResetStore = recoveryStoreAdapter{}
	var _ agui.RecoveryMessenger = telegramRecoveryMessenger{}
	var _ agui.PasswordResetter = authulaPasswordResetter{}
}

type fakePasswordResetServer struct {
	service *agui.PasswordResetService
}

func (s *fakePasswordResetServer) SetPasswordResetService(service *agui.PasswordResetService) {
	s.service = service
}

type fakePasswordResetCoreProvider struct {
	core *authulaservices.CoreServices
}

func (p fakePasswordResetCoreProvider) CoreServices() *authulaservices.CoreServices {
	return p.core
}

type panicPasswordResetCoreProvider struct{}

func (*panicPasswordResetCoreProvider) CoreServices() *authulaservices.CoreServices {
	panic("typed nil provider should be treated as missing")
}

type fakeRecoveryDeliverer struct{}

func (d *fakeRecoveryDeliverer) DeliverToIdentity(context.Context, string, string) (bool, error) {
	return true, nil
}

type fakePasswordResetTokenQueries struct {
	wantTokenHash string
	token         sqlc.AuraPasswordResetTokens
	getErr        error
	consumeErr    error
	incremented   bool
}

func (q *fakePasswordResetTokenQueries) GetPasswordResetToken(_ context.Context, tokenHash string) (sqlc.AuraPasswordResetTokens, error) {
	q.assertTokenHash(tokenHash)
	return q.token, q.getErr
}

func (q *fakePasswordResetTokenQueries) ConsumePasswordResetToken(_ context.Context, tokenHash string) (sqlc.AuraPasswordResetTokens, error) {
	q.assertTokenHash(tokenHash)
	return q.token, q.consumeErr
}

func (q *fakePasswordResetTokenQueries) IncrementPasswordResetTokenAttempts(_ context.Context, tokenHash string) error {
	q.assertTokenHash(tokenHash)
	q.incremented = true
	return nil
}

func (q *fakePasswordResetTokenQueries) assertTokenHash(tokenHash string) {
	if tokenHash != q.wantTokenHash {
		panic("unexpected token hash: " + tokenHash)
	}
}
