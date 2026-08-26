package webauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/Authula/authula/migrations"
	"github.com/Authula/authula/models"
	jwtplugin "github.com/Authula/authula/plugins/jwt"
	"github.com/Authula/authula/plugins/jwt/repositories"
	jwtservices "github.com/Authula/authula/plugins/jwt/services"
	jwtoptions "github.com/Authula/authula/plugins/jwt/types"
	authulaservices "github.com/Authula/authula/services"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const mcpTokenPluginID = "aura_mcp_tokens"
const mcpSubjectClaim = "mcp_subject"

// mcpTokenPlugin reuses Authula's maintained Ed25519/JWKS and rotating-refresh
// implementation without installing the JWT plugin's global post-login hook. That
// hook would mint unrelated bearer tokens on every cockpit login; MCP tokens must
// instead be audience-bound and issued only by the OAuth token endpoint below.
type mcpTokenPlugin struct {
	jwt     jwtservices.TokenService
	refresh jwtservices.RefreshTokenService
	keys    jwtservices.CacheService
}

// Authula rebuilds sub from the login user during refresh and deliberately drops
// the old sub. The carried mcp_subject restores the resource owner's stable tenant
// subject while the login session still controls revocation.
type subjectTokenService struct {
	jwtservices.TokenService
	keys jwtservices.CacheService
}

func (s *subjectTokenService) GenerateUserToken(ctx context.Context, userID, sessionID string, extraClaims map[string]any) (*jwtoptions.TokenPair, error) {
	claims := make(map[string]any, len(extraClaims)+1)
	for key, value := range extraClaims {
		claims[key] = value
	}
	if subject, ok := claims[mcpSubjectClaim].(string); ok && subject != "" {
		claims["sub"] = subject
	}
	return s.TokenService.GenerateUserToken(ctx, userID, sessionID, claims)
}

// Authula v1.42.0 calls jwt.Parse without a verification source, which jwx v3 rejects by
// default. Verify against Authula's own JWKS while preserving the TokenService contract.
// https://pkg.go.dev/github.com/lestrrat-go/jwx/v3/jwt#WithKeySet
func (s *subjectTokenService) ExtractClaims(ctx context.Context, token string) (map[string]any, error) {
	if s.keys == nil {
		return nil, errors.New("webauth: Authula JWKS cache is unavailable")
	}
	keySet, err := s.keys.GetJWKSWithFallback(ctx)
	if err != nil {
		return nil, fmt.Errorf("webauth: load MCP verification keys: %w", err)
	}
	parsed, err := jwt.Parse([]byte(token), jwt.WithKeySet(keySet))
	if err != nil {
		return nil, fmt.Errorf("webauth: verify MCP token: %w", err)
	}
	claims := make(map[string]any, len(parsed.Keys()))
	for _, key := range parsed.Keys() {
		var value any
		if err := parsed.Get(key, &value); err != nil {
			return nil, fmt.Errorf("webauth: read MCP token claim %q: %w", key, err)
		}
		claims[key] = value
	}
	return claims, nil
}

func newMCPTokenPlugin() *mcpTokenPlugin { return &mcpTokenPlugin{} }

func (*mcpTokenPlugin) Metadata() models.PluginMetadata {
	return models.PluginMetadata{
		ID:          mcpTokenPluginID,
		Version:     "1.0.0",
		Description: "Audience-bound OAuth tokens for Aura-owned MCP resources",
	}
}

func (*mcpTokenPlugin) Config() any { return map[string]any{"enabled": true} }

func (p *mcpTokenPlugin) Init(ctx *models.PluginContext) error {
	coreSession, ok := ctx.ServiceRegistry.Get(models.ServiceSession.String()).(authulaservices.SessionService)
	if !ok {
		return errors.New("webauth: Authula session service unavailable for MCP OAuth")
	}
	coreToken, ok := ctx.ServiceRegistry.Get(models.ServiceToken.String()).(authulaservices.TokenService)
	if !ok {
		return errors.New("webauth: Authula token service unavailable for MCP OAuth")
	}

	options := jwtoptions.JWTPluginConfig{Enabled: true}
	options.ApplyDefaults()
	jwksRepo := repositories.NewBunJWKSRepository(ctx.DB)
	refreshRepo := repositories.NewRefreshTokenRepository(ctx.DB)
	keyService := jwtservices.NewKeyService(jwksRepo, ctx.Logger, coreToken, ctx.GetConfig().Secret)
	cacheService := jwtservices.NewCacheService(jwksRepo, nil, ctx.Logger, options.JWKSCacheTTL)
	if err := keyService.GenerateKeysIfMissing(context.Background()); err != nil {
		return fmt.Errorf("webauth: initialize MCP signing key: %w", err)
	}
	if _, err := keyService.RotateKeysIfNeeded(context.Background(), options.KeyRotationInterval,
		options.KeyRotationGracePeriod, cacheService.InvalidateCache); err != nil {
		return fmt.Errorf("webauth: rotate MCP signing key: %w", err)
	}
	if err := cacheService.InvalidateCache(context.Background()); err != nil {
		return fmt.Errorf("webauth: load MCP JWKS: %w", err)
	}

	baseJWTService := jwtservices.NewJWTService(ctx.Logger, coreSession, coreToken, keyService,
		cacheService, nil, options.ExpiresIn, options.RefreshExpiresIn).(jwtservices.TokenService)
	jwtService := &subjectTokenService{TokenService: baseJWTService, keys: cacheService}
	p.jwt = jwtService
	p.refresh = jwtservices.NewRefreshTokenService(ctx.Logger, ctx.EventBus, coreSession,
		jwtService, refreshRepo, options.RefreshGracePeriod, options.RefreshExpiresIn)
	p.keys = cacheService
	return nil
}

func (*mcpTokenPlugin) Migrations(provider string) []migrations.Migration {
	return jwtplugin.JWTMigrationsForProvider(provider)
}

func (*mcpTokenPlugin) DependsOn() []string { return nil }
func (*mcpTokenPlugin) Close() error        { return nil }
