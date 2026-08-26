//go:build arcadedb_integration || mcp_live_integration

package webauth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	authula "github.com/Authula/authula"
	authulaconfig "github.com/Authula/authula/config"
	authulamodels "github.com/Authula/authula/models"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/mcp"
)

// LiveMCPTokenIssuer is the tagged live-test composition of Aura's production
// Authula MCP token plugin. It lets integration tiers authenticate published
// resource servers without adding a test-only token path to those servers.
type LiveMCPTokenIssuer struct {
	auth      *authula.Auth
	tokens    *mcpTokenPlugin
	closeOnce sync.Once
	closeErr  error
}

// NewLiveMCPTokenIssuer initializes only the production MCP token plugin over
// the migrated Authula schema. The caller owns Close.
func NewLiveMCPTokenIssuer(dsn, secret string) (_ *LiveMCPTokenIssuer, err error) {
	isolatedDSN, err := ensureAuthulaSearchPath(dsn)
	if err != nil {
		return nil, fmt.Errorf("webauth: live MCP token issuer DSN: %w", err)
	}
	if err := validateAuthulaSecret(secret); err != nil {
		return nil, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("webauth: live MCP token issuer init: %v", recovered)
		}
	}()

	config := authulaconfig.NewConfig(
		authulaconfig.WithAppName("Aura MCP live integration"),
		authulaconfig.WithBasePath(basePath),
		authulaconfig.WithSecret(secret),
		authulaconfig.WithDatabase(authulamodels.DatabaseConfig{
			Provider: "postgres", URL: isolatedDSN, MaxOpenConns: 2, MaxIdleConns: 1,
		}),
		authulaconfig.WithEventBus(authulamodels.EventBusConfig{
			Provider: "gochannel", GoChannel: &authulamodels.GoChannelConfig{BufferSize: 10},
		}),
		authulaconfig.WithPlugins(authulamodels.PluginsConfig{
			mcpJWTPluginName: map[string]any{"enabled": true},
		}),
	)
	tokens := newMCPTokenPlugin()
	auth := authula.New(&authula.AuthConfig{Config: config, Plugins: []authulamodels.Plugin{tokens}})
	return &LiveMCPTokenIssuer{auth: auth, tokens: tokens}, nil
}

// AccessToken issues an audience-bound access token whose standard subject is
// the tenant UUID exercised by the live resource-server tier.
func (i *LiveMCPTokenIssuer) AccessToken(ctx context.Context, resource, subject string) (string, error) {
	if i == nil || i.tokens == nil || i.tokens.jwt == nil {
		return "", errors.New("webauth: live MCP token issuer is unavailable")
	}
	if _, err := uuid.Parse(subject); err != nil {
		return "", fmt.Errorf("webauth: live MCP token subject: %w", err)
	}
	sessionID := uuid.NewString()
	pair, err := i.tokens.jwt.GenerateUserToken(ctx, "mcp-live-probe", sessionID, map[string]any{
		"iss":           mcp.AuraAuthorizationServerIssuer,
		"aud":           resource,
		"scope":         mcp.AuraOAuthToolsScope,
		"client_id":     mcpOAuthClientID,
		mcpSubjectClaim: subject,
	})
	if err != nil {
		return "", fmt.Errorf("webauth: issue live MCP access token: %w", err)
	}
	return pair.AccessToken, nil
}

// Close releases every Authula resource owned by the tagged live-test issuer.
func (i *LiveMCPTokenIssuer) Close() error {
	if i == nil || i.auth == nil {
		return nil
	}
	i.closeOnce.Do(func() {
		i.closeErr = errors.Join(i.auth.ClosePlugins(), i.auth.CloseSystems())
		if eventBus := i.auth.EventBus(); eventBus != nil {
			i.closeErr = errors.Join(i.closeErr, eventBus.Close())
		}
		if closer, ok := i.auth.DB().(interface{ Close() error }); ok {
			i.closeErr = errors.Join(i.closeErr, closer.Close())
		}
	})
	return i.closeErr
}
