package webauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"maps"
	"slices"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
	jwtservices "github.com/Authula/authula/plugins/jwt/services"
	jwtoptions "github.com/Authula/authula/plugins/jwt/types"
	authulaservices "github.com/Authula/authula/services"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/chetto1983/aura/internal/mcp"
)

const (
	testMCPResource = "http://127.0.0.1:8096/mcp/"
	testMCPSubject  = "b130c94d-a213-463a-a797-ec124104363a"
	testLoginUser   = "authula-login-user"
	testSession     = "session-1"
)

type fixedKeyService struct {
	jwtservices.KeyService
	active *jwtoptions.JWKS
}

func (f fixedKeyService) GetActiveKey(context.Context) (*jwtoptions.JWKS, error) {
	return f.active, nil
}

type plainSecrets struct{ authulaservices.TokenService }

func (plainSecrets) Decrypt(value string) (string, error) { return value, nil }

type fixedJWKSCache struct {
	jwtservices.CacheService
	set jwk.Set
}

func (f fixedJWKSCache) GetJWKSWithFallback(context.Context) (jwk.Set, error) { return f.set, nil }

func newTestMCPTokenService(t *testing.T, refreshTTL time.Duration) *mcpTokenService {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := jwk.Import(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := publicKey.Set(jwk.KeyIDKey, "fixture-key"); err != nil {
		t.Fatal(err)
	}
	if err := publicKey.Set(jwk.AlgorithmKey, "EdDSA"); err != nil {
		t.Fatal(err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(publicKey); err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return &mcpTokenService{
		keys:       fixedKeyService{active: &jwtoptions.JWKS{ID: "fixture-key", PrivateKey: privatePEM}},
		jwks:       fixedJWKSCache{set: set},
		secrets:    plainSecrets{},
		accessTTL:  15 * time.Minute,
		refreshTTL: refreshTTL,
	}
}

func verifiedClaims(t *testing.T, s *mcpTokenService, token string) jwt.Token {
	t.Helper()
	parsed, err := jwt.Parse([]byte(token), jwt.WithKeySet(s.jwks.(fixedJWKSCache).set), jwt.WithValidate(true))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	return parsed
}

func assertMCPContract(t *testing.T, token jwt.Token, wantType string) {
	t.Helper()
	issuer, _ := token.Issuer()
	audience, _ := token.Audience()
	subject, _ := token.Subject()
	var clientID, scope, tokenType string
	_ = token.Get("client_id", &clientID)
	_ = token.Get("scope", &scope)
	_ = token.Get("token_type", &tokenType)
	if issuer != mcp.AuraAuthorizationServerIssuer || !slices.Equal(audience, []string{testMCPResource}) ||
		subject != testMCPSubject || clientID != mcpOAuthClientID || scope != mcp.AuraOAuthToolsScope || tokenType != wantType {
		t.Fatalf("claims iss=%q aud=%v sub=%q client_id=%q scope=%q token_type=%q, want the MCP resource-server contract",
			issuer, audience, subject, clientID, scope, tokenType)
	}
}

func TestMCPTokenServiceCallerCannotOverrideContractClaims(t *testing.T) {
	service := newTestMCPTokenService(t, time.Hour)
	claims := mcpTokenClaims(testMCPResource, mcp.AuraOAuthToolsScope, testMCPSubject)
	maps.Copy(claims, map[string]any{"iss": "https://evil", "aud": "https://evil", "sub": "evil", "client_id": "evil"})

	pair, err := service.GenerateUserToken(t.Context(), testLoginUser, testSession, claims)
	if err != nil {
		t.Fatal(err)
	}
	assertMCPContract(t, verifiedClaims(t, service, pair.AccessToken), "access_token")
	assertMCPContract(t, verifiedClaims(t, service, pair.RefreshToken), "refresh_token")
}

func TestMCPTokenServiceRefusesTokensWithoutResourceOrSubject(t *testing.T) {
	service := newTestMCPTokenService(t, time.Hour)
	for name, claims := range map[string]map[string]any{
		"no resource": {mcpSubjectClaim: testMCPSubject},
		"no subject":  {mcpResourceClaim: testMCPResource},
	} {
		if _, err := service.GenerateUserToken(t.Context(), testLoginUser, testSession, claims); err == nil {
			t.Errorf("%s: token issued", name)
		}
	}
	if _, err := service.GenerateMachineToken(t.Context(), "client", "", nil); err == nil {
		t.Error("machine token issued")
	}
}

func TestMCPTokenServiceValidateRefreshTokenChecksTypeIssuerAndSignature(t *testing.T) {
	service := newTestMCPTokenService(t, -time.Minute)
	pair, err := service.GenerateUserToken(t.Context(), testLoginUser, testSession,
		mcpTokenClaims(testMCPResource, mcp.AuraOAuthToolsScope, testMCPSubject))
	if err != nil {
		t.Fatal(err)
	}
	// Expired on purpose: expiry belongs to the stored record, so reuse of an expired
	// revoked token still reaches Authula's reuse detection.
	claims, err := service.ValidateRefreshToken(t.Context(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("expired refresh token rejected before the stored record was read: %v", err)
	}
	if claims[mcpResourceClaim] != testMCPResource || claims["client_id"] != mcpOAuthClientID {
		t.Fatalf("refresh claims = %v", claims)
	}
	if _, err := service.ValidateRefreshToken(t.Context(), pair.AccessToken); err == nil {
		t.Error("access token accepted as a refresh token")
	}
	tampered := []byte(pair.RefreshToken)
	tampered[len(tampered)-1] ^= 1
	if _, err := service.ValidateRefreshToken(t.Context(), string(tampered)); err == nil {
		t.Error("tampered refresh token accepted")
	}
	key, err := service.signingKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := signMCPToken(key, nil, map[string]any{"iss": "https://other-issuer"},
		jwtoptions.JWTTokenTypeRefresh, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ValidateRefreshToken(t.Context(), foreign); err == nil {
		t.Error("refresh token from another issuer accepted")
	}
}

type liveSession struct {
	authulaservices.SessionService
	deleted []string
}

func (s *liveSession) GetByID(_ context.Context, id string) (*authulamodels.Session, error) {
	if slices.Contains(s.deleted, id) {
		return nil, nil
	}
	return &authulamodels.Session{ID: id, UserID: testLoginUser, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (s *liveSession) Delete(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type memoryRefreshStore struct {
	jwtservices.RefreshTokenRepository
	records map[string]*jwtoptions.RefreshToken
}

func (m *memoryRefreshStore) StoreRefreshToken(_ context.Context, record *jwtoptions.RefreshToken) error {
	m.records[record.TokenHash] = record
	return nil
}

func (m *memoryRefreshStore) GetRefreshToken(_ context.Context, hash string) (*jwtoptions.RefreshToken, error) {
	return m.records[hash], nil
}

func (m *memoryRefreshStore) RotateRefreshToken(_ context.Context, oldHash string, next *jwtoptions.RefreshToken) (bool, error) {
	old := m.records[oldHash]
	if old == nil || old.IsRevoked {
		return false, nil
	}
	revokedAt := time.Now().Add(-time.Second)
	old.IsRevoked, old.RevokedAt = true, &revokedAt
	m.records[next.TokenHash] = next
	return true, nil
}

func (m *memoryRefreshStore) RevokeAllSessionTokens(_ context.Context, sessionID string) error {
	for _, record := range m.records {
		if record.SessionID == sessionID {
			record.IsRevoked = true
		}
	}
	return nil
}

type recordingBlacklist struct {
	jwtservices.BlacklistService
	sessions []string
}

func (b *recordingBlacklist) BlacklistAllSessionTokens(_ context.Context, sessionID string, _ time.Time) error {
	b.sessions = append(b.sessions, sessionID)
	return nil
}

// The claims Authula's v1.50 rotation drops (iss, aud, sub, client_id) must come back on
// every refreshed token, and reuse of a rotated token must revoke the session family.
func TestMCPTokenServiceSurvivesAuthulaRefreshRotation(t *testing.T) {
	service := newTestMCPTokenService(t, time.Hour)
	sessions, store, blacklist := &liveSession{}, &memoryRefreshStore{records: map[string]*jwtoptions.RefreshToken{}}, &recordingBlacklist{}
	refresher := jwtservices.NewRefreshTokenService(slog.New(slog.DiscardHandler), nil, sessions, service, store,
		blacklist, 0, service.accessTTL, service.refreshTTL)

	pair, err := service.GenerateUserToken(t.Context(), testLoginUser, testSession,
		mcpTokenClaims(testMCPResource, mcp.AuraOAuthToolsScope, testMCPSubject))
	if err != nil {
		t.Fatal(err)
	}
	if err := refresher.StoreInitialRefreshToken(t.Context(), pair.RefreshToken, testSession, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	refreshed, err := refresher.RefreshTokens(t.Context(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	assertMCPContract(t, verifiedClaims(t, service, refreshed.AccessToken), "access_token")
	assertMCPContract(t, verifiedClaims(t, service, refreshed.RefreshToken), "refresh_token")

	if _, err := refresher.RefreshTokens(t.Context(), pair.RefreshToken); err == nil {
		t.Fatal("rotated refresh token was accepted again")
	}
	if !slices.Equal(blacklist.sessions, []string{testSession}) || !slices.Equal(sessions.deleted, []string{testSession}) {
		t.Fatalf("reuse after the grace period: blacklisted %v, deleted %v, want the session family revoked",
			blacklist.sessions, sessions.deleted)
	}
}

// A refresh token minted before mcp_resource existed carries the resource only in aud.
func TestMCPTokenServiceRefreshesTokensMintedWithoutResourceClaim(t *testing.T) {
	service := newTestMCPTokenService(t, time.Hour)
	store := &memoryRefreshStore{records: map[string]*jwtoptions.RefreshToken{}}
	refresher := jwtservices.NewRefreshTokenService(slog.New(slog.DiscardHandler), nil, &liveSession{}, service, store,
		&recordingBlacklist{}, 0, service.accessTTL, service.refreshTTL)
	key, err := service.signingKey(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := signMCPToken(key, map[string]any{"scope": mcp.AuraOAuthToolsScope, mcpSubjectClaim: testMCPSubject},
		map[string]any{"iss": mcp.AuraAuthorizationServerIssuer, "aud": testMCPResource, "sub": testMCPSubject,
			"client_id": mcpOAuthClientID, "user_id": testLoginUser, "session_id": testSession},
		jwtoptions.JWTTokenTypeRefresh, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := refresher.StoreInitialRefreshToken(t.Context(), legacy, testSession, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	refreshed, err := refresher.RefreshTokens(t.Context(), legacy)
	if err != nil {
		t.Fatalf("refresh of a pre-#212 token: %v", err)
	}
	assertMCPContract(t, verifiedClaims(t, service, refreshed.AccessToken), "access_token")
}
