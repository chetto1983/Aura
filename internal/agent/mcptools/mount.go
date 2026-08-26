package mcptools

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// MountServer spawns one stdio MCP server, mounts all advertised tools into reg,
// and returns a closer that shuts the subprocess down. On any failure (spawn,
// tools/list, name collision) it returns an error and leaves reg untouched / the
// subprocess reaped, so a misconfigured server can never half-register or leak a
// process. The closer MUST be called at agent shutdown (goleak-clean).
//
// processCtx bounds the spawned subprocess's ENTIRE lifetime (the daemon/boot
// context — must NOT be a short-lived, deferred-cancel context, see Pitfall #2);
// handshakeCtx bounds ONLY this mount attempt (the connect handshake AND the
// mount-time tools/list): a hung handshake is dropped within handshakeCtx's
// deadline without affecting processCtx or any other server sharing it.
func MountServer(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig) (closer func() error, names []string, err error) {
	return mountStdio(processCtx, handshakeCtx, reg, name, cfg)
}

// MountManagedServer opens a managed MCP server (stdio or streamable HTTP,
// resolved from its config), mounts all advertised tools into reg, and returns a
// closer for the underlying transport. processCtx/handshakeCtx follow MountServer's
// two-context contract (Pitfall #2).
func MountManagedServer(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer) (closer func() error, names []string, err error) {
	closer, names, _, err = MountManagedServerWithOptions(
		processCtx,
		handshakeCtx,
		reg,
		name,
		server,
		MountOptions{Egress: mcp.RuntimeEgressPolicy(false, server)},
	)
	return closer, names, err
}

// MountOptions carries the per-mount choices the composition root owns: the
// network policy resolved from the server's runtime profile, and the operator
// consent surface a server-initiated elicitation reaches (plan 45.1-06).
//
// A zero Elicitation is meaningful, not merely absent: it leaves
// SessionOptions.Elicitation nil, and a nil ClientOptions.ElicitationHandler
// means the client does NOT advertise the elicitation capability. That is
// today's honest posture — only a mount with a real consent surface should tell
// servers it can be asked.
type MountOptions struct {
	Egress      mcp.EgressPolicy
	Elicitation ElicitationConsent
	// Views is the process-wide store a mount reads its `ui://` documents into
	// (bridge_views.go). Nil means this host renders nothing, which every
	// *ViewCatalog method tolerates — so a caller with no rendering surface passes
	// nothing rather than each mount path growing an enabled/disabled branch.
	Views *mcp.ViewCatalog
	// OAuth carries the per-identity grant store so a mount can present a token the human
	// already granted. Without it the SDK has no InitialTokenSource and starts a fresh
	// authorization — which, unattended, is refused outright. That is exactly what a
	// restart did on 2026-08-24: Linear and Notion had valid stored grants, refresh tokens
	// and all, and both came back demanding a second consent because the mount was never
	// handed the store that held them.
	//
	// A zero value keeps the old behaviour, which is right for a server that needs no
	// authorization at all.
	OAuth mcp.OAuthOptions
}

// MountManagedServerWithOptions is the one mount entry point that carries the
// full per-mount option set. MountManagedServer is the no-options convenience
// over it; the two egress-only wrappers that used to sit between them were
// deleted when the composition root started needing the mounted host for every
// managed server, not just the memory one.
func MountManagedServerWithOptions(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, opts MountOptions) (closer func() error, names []string, host *MountedServer, err error) {
	policy := managedBridgePolicy(server)
	if isStreamableHTTPManagedServer(server) {
		return mountManagedHTTPHost(processCtx, handshakeCtx, reg, name, server, policy, opts)
	}

	cfg, err := managedStdioConfig(name, server)
	if err != nil {
		return nil, nil, nil, err
	}
	return mountStdioWithPolicyHost(processCtx, handshakeCtx, reg, name, cfg, policy, opts)
}

// elicitationHandlerFor returns the per-mount handler, or nil when no consent
// surface was injected. Returning a typed nil here would be a bug: the SDK tests
// ClientOptions.ElicitationHandler != nil to decide whether to advertise the
// capability, so a non-nil closure wrapping a nil surface would advertise a
// capability that always declines — the "lie told to every mounted server" plan
// 45.1-06 rejected as option C.
func elicitationHandlerFor(name string, consent ElicitationConsent) func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
	if consent == nil {
		return nil
	}
	return NewElicitationHandler(name, consent)
}

func sendingMiddleware(policy bridgePolicy, owner string) []sdkmcp.Middleware {
	if !policy.identityScoped {
		return nil
	}
	return []sdkmcp.Middleware{IdentityBindingMiddleware(owner)}
}

// mountManagedHTTPHost is the streamable-HTTP mirror of mountStdio: it opens the
// raw session and lists tools via handshakeCtx (bounded exactly like mountStdio's
// raw discovery call — same rationale, see bridgeFromAdvertised's doc comment),
// then wraps the session in a MountedServer so every CALL after a successful
// mount gets the redial-on-transport-error behavior the stdio branch already had.
func mountManagedHTTPHost(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, policy bridgePolicy, opts MountOptions) (closer func() error, names []string, host *MountedServer, err error) {
	elicit := elicitationHandlerFor(name, opts.Elicitation)
	connect := func(_ context.Context, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Sending = sendingMiddleware(policy, identityctx.IdentityID(hctx))
		o.Elicitation = elicit
		o.OAuth = opts.OAuth
		return mcp.OpenSDKSession(hctx, name, server, opts.Egress, o)
	}
	if policy.identityScoped {
		return openIdentityScopedHTTPMount(processCtx, handshakeCtx, reg, name, policy, opts.Views, connect)
	}
	var srv *MountedServer
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.ToolListChanged = srv.onToolListChanged
		return connect(pctx, hctx, o)
	}
	srv = NewMountedServer(name, open)
	return openAttachAndMount(srv, processCtx, handshakeCtx, open, reg, name, policy, opts.Views)
}

func openIdentityScopedHTTPMount(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, policy bridgePolicy, views *mcp.ViewCatalog, connect openSessionFunc) (closer func() error, names []string, host *MountedServer, err error) {
	procCtx, cancel := context.WithCancel(processCtx)
	parent := NewMountedServer(name, nil)
	pool := newIdentitySessionPool(parent, connect, procCtx)
	parent.identityPool = pool

	session, advertised, err := pool.openInitial(handshakeCtx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	names, err = mountWithAdvertisedPolicy(reg, name, parent, advertised, policy)
	if err != nil {
		_ = parent.Close()
		cancel()
		return nil, nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	parent.trackAcceptedTools(advertised)
	hydrateViews(handshakeCtx, session, name, views, advertised, policy)
	return func() error {
		defer cancel()
		return parent.Close()
	}, names, parent, nil
}

// mountStdio is the shared stdio mount body for MountServer and MountManagedServer's
// stdio branch. It lists tools via the RAW session (session.Tools), NOT through
// MountedServer's ListTools, so the mount-time discovery call is bounded purely
// by handshakeCtx (see bridgeFromAdvertised's doc comment for why routing it
// through the mounted supervisor here would silently blow the mount deadline).
// The supervisor is still constructed and returned as the mounted tools' owner,
// so every CALL after a successful mount gets the normal redial-on-transport-
// error behavior.
func mountStdio(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig) (closer func() error, names []string, err error) {
	return mountStdioWithPolicy(processCtx, handshakeCtx, reg, name, cfg, defaultBridgePolicy(name))
}

func mountStdioWithPolicy(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, policy bridgePolicy) (closer func() error, names []string, err error) {
	closer, names, _, err = mountStdioWithPolicyHost(
		processCtx,
		handshakeCtx,
		reg,
		name,
		cfg,
		policy,
		MountOptions{},
	)
	return closer, names, err
}

func mountStdioWithPolicyHost(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, policy bridgePolicy, opts MountOptions) (closer func() error, names []string, host *MountedServer, err error) {
	var srv *MountedServer
	elicit := elicitationHandlerFor(name, opts.Elicitation)
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Sending = sendingMiddleware(policy, "")
		o.ToolListChanged = srv.onToolListChanged
		o.Elicitation = elicit
		return mcp.OpenSDKSessionForConfig(pctx, hctx, name, cfg, o)
	}
	srv = NewMountedServer(name, open)
	return openAttachAndMount(srv, processCtx, handshakeCtx, open, reg, name, policy, opts.Views)
}

// openAttachAndMount is the shared body both mount branches reduce to once
// their own openSessionFunc closure is built: open the first session, list its
// tools bounded purely by handshakeCtx (bridgeFromAdvertised's doc comment
// explains why that must NOT route through the supervisor), Attach, then mount
// with the given policy — reaping the session on any failure along the way.
func openAttachAndMount(srv *MountedServer, processCtx, handshakeCtx context.Context, open openSessionFunc, reg *tools.Registry, name string, policy bridgePolicy, views *mcp.ViewCatalog) (closer func() error, names []string, host *MountedServer, err error) {
	// Each mount attempt gets its own cancellable slice of the daemon-lifetime process
	// context, so a mount that gives up reaps the child it spawned. Without this a stdio
	// server that hangs during discovery outlives the mount that dropped it: the process
	// ctx belongs to the whole daemon, nothing else ever kills that child, and the SDK's
	// own session Close blocks on it forever (observed as a goroutine parked in
	// pipeRWC.Close). Aura spawned it, so Aura reaps it. On success ownership passes to
	// the mounted server, whose subprocess must outlive this call — cancelling processCtx
	// still propagates here, which is the D-06/D-07 lifetime contract unchanged.
	procCtx, reapChild := context.WithCancel(processCtx)
	mounted := false
	defer func() {
		if !mounted {
			reapChild()
		}
	}()
	srv.setProcessContext(procCtx)

	session, err := open(procCtx, handshakeCtx, mcp.SessionOptions{})
	if err != nil {
		return nil, nil, nil, err
	}
	advertised, err := drainTools(handshakeCtx, session)
	if err != nil {
		// Released, not Closed: the server that just failed its bounded discovery is
		// the one whose Close would block on it (see mcp.ReleaseSession). Waiting here
		// would hand back the whole deadline the drain bound just saved.
		mcp.ReleaseSession(session)
		return nil, nil, nil, err
	}
	srv.Attach(session)

	names, err = mountWithAdvertisedPolicy(reg, name, srv, advertised, policy)
	if err != nil {
		_ = srv.Close()
		return nil, nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	srv.trackAcceptedTools(advertised)
	// After the mount succeeded, on the SAME raw session and the same bounded
	// handshake budget: a view read that hangs must cost this mount its deadline
	// and nothing more, and it must never undo a mount that already worked.
	hydrateViews(handshakeCtx, session, name, views, advertised, policy)
	mounted = true
	return srv.Close, names, srv, nil
}

// isStreamableHTTPManagedServer resolves server's transport via the single
// canonical mcp.Classify (D-01/MCPH-01), replacing an ad hoc url/type check that
// could silently drift from Classify's own dispatch logic. A Classify error (an
// ambiguous mixed url+command entry, or an internally-inconsistent explicit
// type<->trust combination) resolves to true so the caller's HTTP branch is
// reached instead of the stdio branch: mcp.OpenSDKSession re-classifies and
// surfaces that SAME error, guaranteeing a rejected/ambiguous config can never
// silently fall through to a stdio subprocess spawn (the F-027 class this call
// site must not reintroduce).
func isStreamableHTTPManagedServer(server mcp.ManagedServer) bool {
	serverType, _, err := mcp.Classify(server)
	if err != nil {
		return true
	}
	return serverType == mcp.ServerTypeStreamableHTTP
}

func managedStdioConfig(name string, server mcp.ManagedServer) (mcp.ServerConfig, error) {
	return mcpmanager.RuntimeLaunchConfig(name, server)
}
