package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

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

// selfIssuedReason is why the cockpit offers a shipped sidecar no authorization: the token
// is the keeper's to mint (mcp_first_party_grants.go), never a person's to consent to.
const selfIssuedReason = "Aura issues this sidecar's credential itself and renews it before it expires"

var errSelfIssued = fmt.Errorf("%w: %s", agui.ErrNoAuthorizationFlow, selfIssuedReason)

type mcpAuthorizationStore interface {
	mcp.GrantStore
	Delete(context.Context, string) (bool, error)
}

type mcpAuthService struct {
	store  mcpAuthorizationStore
	flows  *mcp.Flows
	signIn *signInRuntime
}

var _ agui.MCPAuthorizationProvider = (*mcpAuthService)(nil)
var _ agui.MCPAccessTokenProvider = (*mcpAuthService)(nil)

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
	return &mcpAuthService{store: store, flows: flows}, nil
}

// OnAuthorized wires the post-authorization mount. The composition root supplies it
// because this file holds no registry: what it owns is grants and flows.
func (s *mcpAuthService) OnAuthorized(mount func(context.Context, string, mcp.ManagedServer)) {
	s.flows.OnAuthorized(mount)
}

// Close releases flows still parked waiting for a redirect that will not arrive.
func (m *mcpAuthService) Close() {
	if m != nil && m.signIn != nil {
		if err := m.signIn.Close(); err != nil {
			slog.Warn("sign-in browser shutdown incomplete", "error", err)
		}
	}
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
	if selfIssuable(server) {
		return agui.MCPAuthorization{Supported: false, Reason: selfIssuedReason}, nil
	}
	grant, err := m.store.Load(ctx, name)
	if errors.Is(err, mcpoauth.ErrNoGrant) {
		return agui.MCPAuthorization{Supported: true}, nil
	}
	if err != nil {
		return agui.MCPAuthorization{}, err
	}
	return agui.MCPAuthorization{Supported: true, Authorized: grant.RefusedAt.IsZero(), Expired: !grant.RefusedAt.IsZero(), ExpiresAt: grant.ExpiresAt}, nil
}

func authorizationServer(name string) (mcp.ManagedServer, error) {
	server, ok, err := effectiveManagedMCPServer(name)
	if err != nil {
		return mcp.ManagedServer{}, err
	}
	if !ok {
		return mcp.ManagedServer{}, fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	if selfIssuable(server) {
		return mcp.ManagedServer{}, errSelfIssued
	}
	return server, nil
}

func (m *mcpAuthService) StartLoopbackAuthorization(ctx context.Context, owner, name string) (mcp.Flow, error) {
	if m.signIn != nil {
		if err := m.signIn.login.Cancel(ctx, owner, ""); err != nil {
			return mcp.Flow{}, err
		}
	}
	server, err := authorizationServer(name)
	if err != nil {
		return mcp.Flow{}, err
	}
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return mcp.Flow{}, err
	}
	return m.flows.Start(ctx, owner, name, server, settings.SignInRedirect())
}

func (m *mcpAuthService) PasteAuthorization(owner, id, address string) (mcp.Flow, error) {
	return m.flows.Paste(owner, id, address)
}

func (m *mcpAuthService) AuthorizationFlow(owner, id string) (mcp.Flow, error) {
	return m.flows.Status(owner, id)
}

func (m *mcpAuthService) RevokeAuthorization(ctx context.Context, name string) (bool, error) {
	server, ok, err := effectiveManagedMCPServer(name)
	if err != nil {
		return false, err
	}
	// A server that is gone from the registry still leaves its grant for the operator to drop.
	if ok && selfIssuable(server) {
		return false, errSelfIssued
	}
	return m.store.Delete(ctx, name)
}

func (m *mcpAuthService) AccessToken(ctx context.Context, name string) (string, error) {
	server, ok, err := effectiveManagedMCPServer(name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("MCP server %q is not configured or is disabled", name)
	}
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	if err != nil {
		return "", err
	}
	if !mcp.UsesOAuth(server, settings) {
		return "", fmt.Errorf("MCP server %q does not use OAuth", name)
	}
	return mcp.StoredOAuthAccessToken(ctx, name, m.store, runtimeMCPEgressPolicy(server), nil)
}
