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
)

const mcpJWTPluginName = "aura_mcp_" + "tokens"

// mcpTokenPlugin reuses Authula's maintained Ed25519/JWKS and rotating-refresh
// implementation without installing the JWT plugin's global post-login hook. That
// hook would mint unrelated bearer tokens on every cockpit login; MCP tokens must
// instead be audience-bound and issued only by the OAuth token endpoint below.
type mcpTokenPlugin struct {
	jwt     jwtservices.TokenService
	refresh jwtservices.RefreshTokenService
	keys    jwtservices.CacheService
}

func newMCPTokenPlugin() *mcpTokenPlugin { return &mcpTokenPlugin{} }

func (*mcpTokenPlugin) Metadata() models.PluginMetadata {
	return models.PluginMetadata{
		ID:          mcpJWTPluginName,
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
	// Authula v1.50 blacklists the whole session when a revoked refresh token is reused
	// after the grace period, and dereferences the blacklist without a nil check.
	storage, ok := ctx.ServiceRegistry.Get(models.ServiceSecondaryStorage.String()).(authulaservices.SecondaryStorageService)
	if !ok {
		return errors.New("webauth: Authula secondary storage unavailable for MCP refresh-token reuse detection")
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

	tokens := &mcpTokenService{keys: keyService, jwks: cacheService, secrets: coreToken,
		accessTTL: options.ExpiresIn, refreshTTL: options.RefreshExpiresIn}
	p.jwt = tokens
	p.refresh = jwtservices.NewRefreshTokenService(ctx.Logger, ctx.EventBus, coreSession, tokens, refreshRepo,
		jwtservices.NewBlacklistService(storage.GetStorage(), ctx.Logger),
		options.RefreshGracePeriod, options.ExpiresIn, options.RefreshExpiresIn)
	p.keys = cacheService
	return nil
}

func (*mcpTokenPlugin) Migrations(provider string) []migrations.Migration {
	return jwtplugin.JWTMigrationsForProvider(provider)
}

func (*mcpTokenPlugin) DependsOn() []string { return nil }
func (*mcpTokenPlugin) Close() error        { return nil }
