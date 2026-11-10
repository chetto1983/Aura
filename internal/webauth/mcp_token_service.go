package webauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	jwtservices "github.com/Authula/authula/plugins/jwt/services"
	jwtoptions "github.com/Authula/authula/plugins/jwt/types"
	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/chetto1983/aura/internal/mcp"
)

// mcpResourceClaim and mcpSubjectClaim carry the audience and the tenant through
// Authula's refresh rotation, which drops iss, aud, sub and client_id from the claims
// it hands back to GenerateUserToken.
// https://github.com/Authula/authula/blob/v1.50.1/plugins/jwt/services/refresh_token_service.go
const (
	mcpResourceClaim = "mcp_resource"
	mcpSubjectClaim  = "mcp_subject"
)

// mcpTokenService signs Aura's MCP tokens with Authula's keys. Authula's own token
// service (v1.50) overwrites iss, aud and sub and refuses client_id, but the MCP resource
// servers read exactly those four: aud is the resource URL and sub is the Aura identity,
// not the Authula login user. Keys, JWKS, refresh storage, rotation and reuse detection
// stay Authula's: this type is the TokenService its RefreshTokenService calls back into.
type mcpTokenService struct {
	keys       jwtservices.KeyService
	jwks       jwtservices.CacheService
	secrets    authulaservices.TokenService
	accessTTL  time.Duration
	refreshTTL time.Duration
}

var _ jwtservices.TokenService = (*mcpTokenService)(nil)

func (s *mcpTokenService) GenerateUserToken(ctx context.Context, userID, sessionID string, extraClaims map[string]any) (*jwtoptions.TokenPair, error) {
	resource, _ := extraClaims[mcpResourceClaim].(string)
	subject, _ := extraClaims[mcpSubjectClaim].(string)
	if resource == "" || subject == "" || userID == "" || sessionID == "" {
		return nil, errors.New("webauth: MCP token needs a resource, a subject, a user and a session")
	}
	key, err := s.signingKey(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	fixed := map[string]any{
		jwt.IssuerKey: mcp.AuraAuthorizationServerIssuer, jwt.AudienceKey: resource,
		jwt.SubjectKey: subject, "client_id": mcpOAuthClientID, "user_id": userID,
		"session_id": sessionID, "actor_type": "user", jwt.IssuedAtKey: now,
	}
	access, err := signMCPToken(key, extraClaims, fixed, jwtoptions.JWTTokenTypeAccess, now.Add(s.accessTTL))
	if err != nil {
		return nil, err
	}
	refresh, err := signMCPToken(key, extraClaims, fixed, jwtoptions.JWTTokenTypeRefresh, now.Add(s.refreshTTL))
	if err != nil {
		return nil, err
	}
	return &jwtoptions.TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: s.accessTTL, TokenType: "Bearer"}, nil
}

func (*mcpTokenService) GenerateMachineToken(context.Context, string, string, []string) (*jwtoptions.TokenPair, error) {
	return nil, errors.New("webauth: MCP tokens are issued to users only")
}

// ValidateRefreshToken verifies the signature, issuer and token type. Expiry is left to
// the stored record, as Authula's own implementation does, so that reuse of an expired
// revoked token is still detected as reuse.
func (s *mcpTokenService) ValidateRefreshToken(ctx context.Context, token string) (map[string]any, error) {
	keySet, err := s.jwks.GetJWKSWithFallback(ctx)
	if err != nil {
		return nil, fmt.Errorf("webauth: load MCP verification keys: %w", err)
	}
	parsed, err := jwt.Parse([]byte(token), jwt.WithKeySet(keySet), jwt.WithValidate(true),
		jwt.WithResetValidators(true), jwt.WithIssuer(mcp.AuraAuthorizationServerIssuer),
		jwt.WithClaimValue("token_type", jwtoptions.JWTTokenTypeRefresh.String()))
	if err != nil {
		return nil, fmt.Errorf("webauth: verify MCP refresh token: %w", err)
	}
	claims := make(map[string]any, len(parsed.Keys()))
	for _, name := range parsed.Keys() {
		var value any
		if err := parsed.Get(name, &value); err != nil {
			return nil, fmt.Errorf("webauth: read MCP token claim %q: %w", name, err)
		}
		claims[name] = value
	}
	// Refresh tokens minted before amendment #212 carry the resource only in `aud`, which
	// Authula drops before GenerateUserToken. Without this they would all fail their next
	// refresh and every browser-authorized grant would need a new sign-in. They expire with
	// the 7-day refresh TTL, after which this branch is dead and can go.
	if audience, _ := parsed.Audience(); claims[mcpResourceClaim] == nil && len(audience) == 1 {
		claims[mcpResourceClaim] = audience[0]
	}
	return claims, nil
}

func (s *mcpTokenService) signingKey(ctx context.Context) (jwk.Key, error) {
	active, err := s.keys.GetActiveKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("webauth: load MCP signing key: %w", err)
	}
	pem, err := s.secrets.Decrypt(active.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("webauth: decrypt MCP signing key: %w", err)
	}
	key, err := jwk.ParseKey([]byte(pem), jwk.WithPEM(true))
	if err != nil {
		return nil, fmt.Errorf("webauth: parse MCP signing key: %w", err)
	}
	if err := key.Set(jwk.KeyIDKey, active.ID); err != nil {
		return nil, fmt.Errorf("webauth: set MCP signing key id: %w", err)
	}
	return key, nil
}

// signMCPToken applies the caller's claims first and the fixed ones last, so a caller
// claim can never replace the issuer, audience, subject or client.
func signMCPToken(key jwk.Key, extra, fixed map[string]any, kind jwtoptions.JWTTokenType, expiresAt time.Time) (string, error) {
	token := jwt.New()
	for _, claims := range []map[string]any{extra, fixed, {
		"token_type": kind.String(), jwt.ExpirationKey: expiresAt, jwt.JwtIDKey: uuid.NewString(),
	}} {
		for name, value := range claims {
			if err := token.Set(name, value); err != nil {
				return "", fmt.Errorf("webauth: set MCP token claim %q: %w", name, err)
			}
		}
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.EdDSA(), key))
	if err != nil {
		return "", fmt.Errorf("webauth: sign MCP %s token: %w", kind, err)
	}
	return string(signed), nil
}
