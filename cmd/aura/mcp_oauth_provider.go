package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

// mcp_oauth_provider.go is the composition root for cockpit-driven MCP authorization: the
// one place holding the grant store, the flow registry and the runtime egress policy at
// the same time, which is why the agui handlers depend on an interface instead of
// assembling any of it themselves.

// mcpOAuthCallbackAPIPath must match the route registered in
// internal/agui/mcp_oauth_api.go. It is appended to the cockpit's origin to form the
// redirect URI the authorization server sends the human back to.
const mcpOAuthCallbackAPIPath = "/api/governance/mcp/authorization/callback"

type mcpAuthService struct {
	store *mcpoauth.Store
	flows *mcp.Flows
	// configuredCallback is the AURA_WEB_PUBLIC_URL override, empty when unset. It is
	// an override and NOT a requirement: see callbackFor.
	configuredCallback string
}

var _ agui.MCPAuthorizationProvider = (*mcpAuthService)(nil)

// newMCPAuthService builds the provider, or returns nil when the deployment cannot
// support it. Nil is not a failure: the routes answer 503 and the rest of the cockpit
// works, which is the right outcome for a deployment with no Postgres or no secret.
func newMCPAuthService(cfg *config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*mcpAuthService, error) {
	if pool == nil || strings.TrimSpace(cfg.AuthulaSecret) == "" {
		return nil, nil
	}
	store, err := mcpoauth.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		return nil, err
	}
	// The opener is where the runtime egress policy is applied, and the reason
	// mcp.Flows carries none: a mount's network policy is resolved here already, and a
	// second resolver would be a second answer to drift away from this one.
	opener := func(ctx context.Context, name string, server mcp.ManagedServer, opts mcp.SessionOptions) (io.Closer, error) {
		session, err := mcp.OpenSDKSession(ctx, name, server, runtimeMCPEgressPolicy(server), opts)
		if err != nil {
			// Returned explicitly rather than falling through: a typed nil pointer
			// inside a non-nil io.Closer is the classic way a caller's `!= nil` check
			// passes and then panics on Close.
			return nil, err
		}
		return session, nil
	}
	flows, err := mcp.NewFlows(store, opener, logger)
	if err != nil {
		return nil, err
	}
	configured, err := configuredCallbackURL(cfg.WebPublicURL)
	if err != nil {
		return nil, err
	}
	return &mcpAuthService{store: store, flows: flows, configuredCallback: configured}, nil
}

// configuredCallbackURL validates the optional AURA_WEB_PUBLIC_URL override.
func configuredCallbackURL(publicURL string) (string, error) {
	trimmed := strings.TrimSpace(publicURL)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("AURA_WEB_PUBLIC_URL must be an absolute origin like https://aura.example, got %q", publicURL)
	}
	return strings.TrimRight(trimmed, "/") + mcpOAuthCallbackAPIPath, nil
}

// callbackFor decides where the authorization server sends the human back.
//
// NO PUBLIC URL IS REQUIRED, and assuming one was the first design's mistake. The
// redirect is never FETCHED by the authorization server — it is an address the human's
// own browser is sent to — so the cockpit's own origin is a valid destination even when
// it is http://localhost:8080 and reachable from nowhere else on the internet. Providers
// accept exactly this: loopback redirects are the documented shape for a native client.
//
// The origin therefore comes from the browser that started the flow, which is the same
// browser that will come back. AURA_WEB_PUBLIC_URL overrides it, and is worth setting for
// two cases the request cannot answer: a pre-registered client whose redirect URI was
// registered by hand at the provider and must match byte for byte, and a deployment
// behind a proxy that rewrites the origin.
func (m *mcpAuthService) callbackFor(origin string) (string, error) {
	if m.configuredCallback != "" {
		return m.configuredCallback, nil
	}
	trimmed := strings.TrimSpace(origin)
	if trimmed == "" {
		return "", errors.New("could not tell which address this cockpit is reached on; set AURA_WEB_PUBLIC_URL")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("the cockpit origin %q is not an absolute URL; set AURA_WEB_PUBLIC_URL", origin)
	}
	return strings.TrimRight(trimmed, "/") + mcpOAuthCallbackAPIPath, nil
}

// Close releases flows still parked waiting for a redirect that will not arrive.
func (m *mcpAuthService) Close() {
	if m != nil && m.flows != nil {
		m.flows.Close()
	}
}

func (m *mcpAuthService) AuthorizationState(ctx context.Context, name string) (agui.MCPAuthorization, error) {
	server, ok, err := effectiveManagedMCPServer(name)
	if err != nil {
		return agui.MCPAuthorization{}, err
	}
	if !ok {
		return agui.MCPAuthorization{}, fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return agui.MCPAuthorization{}, err
	}
	if !mcp.UsesOAuth(server, settings) {
		return agui.MCPAuthorization{Supported: false, Reason: noOAuthReason(server, settings)}, nil
	}
	grant, err := m.store.Load(ctx, name)
	if errors.Is(err, mcpoauth.ErrNoGrant) {
		return agui.MCPAuthorization{Supported: true}, nil
	}
	if err != nil {
		return agui.MCPAuthorization{}, err
	}
	return agui.MCPAuthorization{Supported: true, Authorized: true, ExpiresAt: grant.ExpiresAt}, nil
}

func (m *mcpAuthService) StartAuthorization(ctx context.Context, owner, name, origin string) (mcp.Flow, error) {
	callback, err := m.callbackFor(origin)
	if err != nil {
		return mcp.Flow{}, err
	}
	server, ok, err := effectiveManagedMCPServer(name)
	if err != nil {
		return mcp.Flow{}, err
	}
	if !ok {
		return mcp.Flow{}, fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	return m.flows.Start(ctx, owner, name, server, callback)
}

func (m *mcpAuthService) AuthorizationFlow(owner, id string) (mcp.Flow, error) {
	return m.flows.Status(owner, id)
}

func (m *mcpAuthService) CompleteAuthorization(state, code, iss string) (mcp.Flow, error) {
	return m.flows.Complete(state, code, iss)
}

func (m *mcpAuthService) FailAuthorization(state string, reason error) (mcp.Flow, error) {
	return m.flows.Fail(state, reason)
}

func (m *mcpAuthService) RevokeAuthorization(ctx context.Context, name string) (bool, error) {
	return m.store.Delete(ctx, name)
}
