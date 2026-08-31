package main

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcpoauth"
	"github.com/chetto1983/aura/internal/redact"
)

// liveMCPMount owns post-listener OAuth mounting. The mounted host exposes one shared tool
// schema while mcptools keeps a separate authenticated session for each calling identity.
type liveMCPMount struct {
	reg        *tools.Registry
	handles    *runtimeToolHandles
	strict     bool
	processCtx context.Context

	mu      sync.Mutex
	closers map[string]func() error
	hosts   map[string]*mcptools.MountedServer
	owners  map[string]string
	closed  bool
	wg      sync.WaitGroup
}

type deferOAuthMountsContextKey struct{}

func deferOAuthMountsUntilListener(ctx context.Context) context.Context {
	return context.WithValue(ctx, deferOAuthMountsContextKey{}, true)
}

func newLiveMCPMount(processCtx context.Context, chat *chatEnv) *liveMCPMount {
	return &liveMCPMount{
		reg:        chat.reg,
		handles:    &chat.toolHandles,
		strict:     chat.cfg.Profile.Strict(),
		processCtx: processCtx,
		closers:    map[string]func() error{},
		hosts:      map[string]*mcptools.MountedServer{},
		owners:     map[string]string{},
	}
}

func deferredOAuthMount(server mcp.ManagedServer) bool {
	settings, err := mcp.OAuthSettingsFromEnv(server.Env)
	return err == nil && mcp.UsesOAuth(server, settings)
}

func shouldDeferOAuthMount(ctx context.Context, server mcp.ManagedServer) bool {
	deferUntilListener, _ := ctx.Value(deferOAuthMountsContextKey{}).(bool)
	return deferUntilListener && deferredOAuthMount(server)
}

// StartReconnect mounts stored OAuth grants without delaying daemon startup.
func (m *liveMCPMount) StartReconnect(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) {
	if m == nil || pool == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		_, policies, err := mcpRuntimeSet()
		if err != nil {
			slog.Warn("mcp oauth: load deferred mounts", "err", err)
			return
		}
		names := deferredOAuthMountNames(policies)
		for _, name := range names {
			ownerCtx := mcpOwnerContext(ctx, cfg, pool, name, policies[name])
			if identityctx.IdentityID(ownerCtx) == "" {
				// SAY SO. This skip used to be a bare `continue`, and mcpOwnerContext's
				// own ErrNoGrant branch is deliberately silent too, so a server that
				// never mounted produced no line at all -- not "mounted", not "mount
				// failed", nothing. On 2026-08-31 that cost a live afternoon: memory was
				// absent from the registry, the daemon sat unhealthy on "required memory
				// capability is not mounted", and the only evidence of WHY was in the
				// conversation rows, where the agent had asked tool_search for
				// memory_recall three times and been told it is not a registered tool.
				// A capability silently missing from the manifest is the one failure the
				// model cannot route around, so it costs one WARN.
				slog.Warn("mcp oauth: no grant owner resolved — server NOT mounted, its tools are absent from the agent registry",
					"server", redact.Line(name))
				continue
			}
			m.Mount(ownerCtx, name, policies[name])
		}
	}()
}

func deferredOAuthMountNames(policies map[string]mcp.ManagedServer) []string {
	names := make([]string, 0, len(policies))
	for name, server := range policies {
		if deferredOAuthMount(server) {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		firstI := mcpmanager.FirstPartyRecipe(policies[names[i]])
		firstJ := mcpmanager.FirstPartyRecipe(policies[names[j]])
		if firstI != firstJ {
			return firstI
		}
		return names[i] < names[j]
	})
	return names
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
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return
	}
	m.unmountLocked(name)

	handshakeCtx, cancel := context.WithTimeout(ctx, mcpMountTimeout())
	defer cancel()

	var host *mcptools.MountedServer
	mountOnce := func(attemptCtx context.Context) (func() error, []string, error) {
		closer, mounted, opened, err := mcptools.MountManagedServerWithOptions(
			m.processCtx,
			attemptCtx,
			m.reg,
			name,
			server,
			mcptools.MountOptions{
				Egress: mcp.RuntimeEgressPolicy(m.strict, server),
				Views:  m.handles.MCPViews,
				OAuth:  runtimeMCPOAuth(ctx),
			},
		)
		if err == nil {
			host = opened
		}
		return closer, mounted, err
	}
	closer, mounted, err := mcptools.MountWithRetry(
		handshakeCtx, name, mcpMountRetryPolicy(), mountOnce)
	if err != nil {
		slog.Warn("mcp live mount failed", "server", redact.Line(name), "err", err)
		return
	}
	slog.Info("mcp live mounted", "server", redact.Line(name), "tools", len(mounted))

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = closer()
		return
	}
	m.closers[name] = closer
	m.hosts[name] = host
	m.owners[name] = identityctx.IdentityID(ctx)
	if host != nil && mcp.IsSharedAdminGoverned(server) && m.handles != nil {
		m.handles.MemoryContext.setClient(host)
	}
	m.mu.Unlock()

	if host != nil && m.handles.MCPViews != nil && m.handles.MCPViews.HasServer(name) {
		m.handles.ViewCallers[name] = host
	}
}

func (m *liveMCPMount) Host(name string) *mcptools.MountedServer {
	host, _ := m.OwnedHost(name)
	return host
}

// OwnedHost returns the live schema host together with the identity whose grant opened
// its initial session. Host-level functional probes must reuse that subject: inventing a
// synthetic identity would ask the OAuth session pool to open a grant that cannot exist.
func (m *liveMCPMount) OwnedHost(name string) (*mcptools.MountedServer, string) {
	if m == nil {
		return nil, ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts[name], m.owners[name]
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
		slog.Info("mcp live unmounted", "server", redact.Line(name), "tools", dropped)
	}
	m.mu.Lock()
	closer := m.closers[name]
	host := m.hosts[name]
	delete(m.closers, name)
	delete(m.hosts, name)
	delete(m.owners, name)
	if m.handles != nil {
		m.handles.MemoryContext.clearClient(host)
	}
	m.mu.Unlock()
	if closer != nil {
		_ = closer()
	}
	if m.handles != nil {
		delete(m.handles.ViewCallers, name)
	}
}

// Close prevents new mounts, joins the deferred reconnect, and closes every session
// created after boot. Boot-time MCP sessions remain owned by chatEnv.mcpClosers.
func (m *liveMCPMount) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.wg.Wait()

	m.mu.Lock()
	closers := make([]func() error, 0, len(m.closers))
	for name, closer := range m.closers {
		closers = append(closers, closer)
		if m.handles != nil {
			m.handles.MemoryContext.clearClient(m.hosts[name])
		}
	}
	m.closers = map[string]func() error{}
	m.hosts = map[string]*mcptools.MountedServer{}
	m.owners = map[string]string{}
	m.mu.Unlock()
	return closeMCPServers(closers)
}

// mcpOwnerContext binds an identity that authorized this server so the post-listener
// reconnect can discover its tool schema with a stored grant.
//
// It is a no-op for everything else: a server with no OAuth, a deployment with no pool or
// no secret, and a server nobody has authorized all return ctx unchanged.
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
	owner, err := mcpGrantOwner(ctx, store, name, server, candidates)
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

// mcpGrantOwnerStore is the grant store narrowed to the two owner lookups, so the
// first-party branch below is provable without a live Postgres.
type mcpGrantOwnerStore interface {
	OwnerOf(ctx context.Context, serverName string, candidates []string) (string, error)
	OwnersOf(ctx context.Context, serverName string, candidates []string) ([]string, error)
}

// mcpGrantOwner picks the identity whose stored grant opens this server's process-wide
// mount.
//
// For anything Aura does not own, that identity must be unique: OwnerOf refuses two
// owners rather than hand one person's tools the other person's token.
//
// For Aura's OWN sidecars every identity is meant to hold a grant — the token subject IS
// the memory/calendar/WhatsApp tenant — and the boot mount only reads the tool SCHEMA,
// which is the same for all of them. Each caller's tools then run on that caller's own
// session (identitySessionPool), and IdentityBindingMiddleware rejects a call arriving on
// somebody else's. So the pick here is deliberately the FIRST candidate: ListIdentities
// orders by created_at, so it is the deployment's oldest identity, and it is stable
// across boots. Without this, enrolling a second person would silently unmount memory,
// calendar and WhatsApp for everyone — the same "not mounted" failure this whole path
// exists to prevent.
func mcpGrantOwner(ctx context.Context, store mcpGrantOwnerStore, name string, server mcp.ManagedServer, candidates []string) (string, error) {
	if !mcpmanager.FirstPartyRecipe(server) {
		return store.OwnerOf(ctx, name, candidates)
	}
	owners, err := store.OwnersOf(ctx, name, candidates)
	if err != nil {
		return "", err
	}
	return owners[0], nil
}
