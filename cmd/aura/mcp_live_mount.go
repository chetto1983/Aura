package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcpoauth"
)

// mcp_live_mount.go mounts an MCP server into a RUNNING daemon.
//
// Boot mounts every server it can, and for most that is the whole story. It cannot be the
// whole story for a server behind OAuth: boot has no browser and no identity, so the mount
// fails with "this identity has not authorized this server" and the server is dropped. The
// human then authorizes it from the cockpit — and until this file existed, nothing mounted
// it. Measured 2026-08-24 with Linear: consent completed, the grant landed in
// aura.identity_mcp_oauth correctly, and the agent had zero Linear tools until somebody
// restarted the daemon, with nothing on screen to say a restart was needed.
//
// LibreChat splits the same way and answers it the same way: connections for servers that
// need no user credentials are made at start-up, and a server that requires OAuth or
// user-supplied variables gets a connection created when the user is there to authorize it
// (requiresUserScopedConnection → getUserConnection, packages/api/src/mcp/MCPManager.ts).

// liveMCPMount owns post-boot mounting for the process registry.
//
// Aura runs one registry per process rather than LibreChat's per-user connection pool,
// which is correct for a single-operator appliance and would not be for a multi-tenant
// one: two identities authorizing the same server would share one mount and therefore one
// identity's token. That is a real limit, recorded here rather than hidden — it is why
// this type exists at all instead of a map keyed by identity.
type liveMCPMount struct {
	reg     *tools.Registry
	handles runtimeToolHandles
	strict  bool

	mu      sync.Mutex
	closers map[string]func() error
}

func newLiveMCPMount(chat *chatEnv) *liveMCPMount {
	return &liveMCPMount{
		reg:     chat.reg,
		handles: chat.toolHandles,
		strict:  chat.cfg.Profile.Strict(),
		closers: map[string]func() error{},
	}
}

// Mount brings a server's tools into the live registry, replacing any earlier mount of the
// same server.
//
// ctx is the caller's, and it is used for the handshake only: the session itself must
// outlive the request that triggered the mount, so the process context is what the
// transport is given. That is the same split boot makes between handshakeCtx and ctx.
func (m *liveMCPMount) Mount(ctx context.Context, name string, server mcp.ManagedServer) {
	if m == nil || m.reg == nil {
		return
	}
	m.unmountLocked(name)

	handshakeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mcpMountTimeout())
	defer cancel()

	closer, mounted, host, err := mcptools.MountManagedServerWithOptions(
		context.WithoutCancel(ctx),
		handshakeCtx,
		m.reg,
		name,
		server,
		mcptools.MountOptions{
			Egress: mcp.RuntimeEgressPolicy(m.strict, server),
			Views:  m.handles.MCPViews,
			OAuth:  runtimeMCPOAuth(ctx),
		},
	)
	if err != nil {
		slog.Warn("mcp live mount failed", "server", name, "err", err)
		return
	}
	slog.Info("mcp live mounted", "server", name, "tools", len(mounted))

	m.mu.Lock()
	m.closers[name] = closer
	m.mu.Unlock()

	if host != nil && m.handles.MCPViews != nil && m.handles.MCPViews.HasServer(name) {
		m.handles.ViewCallers[name] = host
	}
}

// Mounted reports whether this server's tools are in the registry right now — the live
// truth the board renders instead of guessing from the config.
func (m *liveMCPMount) Mounted(name string) bool {
	if m == nil || m.reg == nil {
		return false
	}
	return m.reg.HasPrefix(name + "__")
}

// Unmount drops a server's tools and closes its session. A server the operator disabled or
// removed must stop being offered to the model without waiting for a restart — the same
// staleness, in the other direction.
func (m *liveMCPMount) Unmount(name string) {
	if m == nil || m.reg == nil {
		return
	}
	m.unmountLocked(name)
}

func (m *liveMCPMount) unmountLocked(name string) {
	// The bridge namespaces every tool as <server>__<tool> and refuses to register over an
	// existing name, so a remount MUST clear the previous one first or it fails as a
	// collision with itself.
	if dropped := m.reg.Forget(name + "__"); dropped > 0 {
		slog.Info("mcp live unmounted", "server", name, "tools", dropped)
	}
	m.mu.Lock()
	closer := m.closers[name]
	delete(m.closers, name)
	m.mu.Unlock()
	if closer != nil {
		_ = closer()
	}
	delete(m.handles.ViewCallers, name)
}

// mcpOwnerContext binds the identity that authorized this server, so a boot-time mount can
// present a stored grant instead of demanding a fresh consent.
//
// It is a no-op for everything else: a server with no OAuth, a deployment with no pool or
// no secret, and a server nobody has authorized all return ctx unchanged, and the mount
// then fails the way it already did — with the instruction to authorize it.
//
// A server two identities have authorized is deliberately left unmounted at boot. One
// process-wide registry would give both identities' tool calls whichever token was resolved
// first, and a wrong owner here is worse than a missing mount, which at least says so.
func mcpOwnerContext(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, name string, server mcp.ManagedServer) context.Context {
	if pool == nil || strings.TrimSpace(cfg.AuthulaSecret) == "" {
		return ctx
	}
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	if err != nil || !mcp.UsesOAuth(server, settings) {
		return ctx
	}
	store, err := mcpoauth.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		return ctx
	}

	identities, err := identity.New(pool).ListIdentities(ctx)
	if err != nil {
		slog.Warn("mcp oauth: could not list identities to resolve the grant owner", "server", name, "err", err)
		return ctx
	}
	candidates := make([]string, 0, len(identities))
	for _, id := range identities {
		if !id.Deactivated {
			candidates = append(candidates, id.ID)
		}
	}
	owner, err := store.OwnerOf(ctx, name, candidates)
	if err != nil {
		if !errors.Is(err, mcpoauth.ErrNoGrant) {
			slog.Warn("mcp oauth: could not resolve the grant owner", "server", name, "err", err)
		}
		return ctx
	}
	// Only the identity is bound here; the credentials come from runtimeMCPOAuth, which
	// every session-opening path shares. And the identity is bound only once an owner is
	// KNOWN: the grant store refuses an identity-less context by design, so a context
	// carrying somebody arbitrary — or a store handed out with nobody at all — turns every
	// internal sidecar's mount into "no identity on context". Measured 2026-08-24, when
	// calendar spent 40 retries on it for a server that has no authorization to do.
	slog.Info("mcp oauth: mounting with a stored grant", "server", name, "identity", owner)
	return identityctx.WithIdentityID(ctx, owner)
}
