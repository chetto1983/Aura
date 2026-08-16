package mcptools

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
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
	return MountManagedServerWithEgress(
		processCtx,
		handshakeCtx,
		reg,
		name,
		server,
		mcp.RuntimeEgressPolicy(false, server),
	)
}

// MountManagedServerWithEgress mounts a managed server with the network policy
// resolved by the composition root from its runtime profile.
func MountManagedServerWithEgress(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, egress mcp.EgressPolicy) (closer func() error, names []string, err error) {
	closer, names, _, err = MountManagedServerHostWithEgress(
		processCtx,
		handshakeCtx,
		reg,
		name,
		server,
		egress,
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
}

// MountManagedServerHostWithEgress is MountManagedServerWithEgress plus the
// process-owned host view used by trusted daemon integrations such as automatic
// Memory recall and readiness. It mounts without a consent surface.
func MountManagedServerHostWithEgress(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, egress mcp.EgressPolicy) (closer func() error, names []string, host *MountedServer, err error) {
	return MountManagedServerWithOptions(processCtx, handshakeCtx, reg, name, server, MountOptions{Egress: egress})
}

// MountManagedServerWithOptions is the one mount entry point that carries the
// full per-mount option set. The four older exported entry points delegate here
// with a zero Elicitation, so adding the consent surface stayed additive instead
// of churning every signature.
func MountManagedServerWithOptions(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, opts MountOptions) (closer func() error, names []string, host *MountedServer, err error) {
	policy := managedBridgePolicy(server)
	if isStreamableHTTPManagedServer(server) {
		return mountManagedHTTPHost(processCtx, handshakeCtx, reg, name, server, policy, opts)
	}

	cfg, err := managedStdioConfig(name, server)
	if err != nil {
		return nil, nil, nil, err
	}
	return mountStdioWithPolicyHost(processCtx, handshakeCtx, reg, name, cfg, policy, opts.Elicitation)
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

// sendingMiddleware is the sending middleware stack every Aura MCP session opens
// with: the _meta.aura operation stamp (plan 45.1-02) always, plus the _meta.aura
// identity stamp (plan 45.1-05/D-108) ONLY when policy.memory — the middleware is
// registered once on the per-mount Client, so scoping is by mount, not by a
// per-call branch. It is the same function for both mount branches so a policy
// fix in one cannot miss the other.
//
// Order is {OperationMetaMiddleware(), IdentityMetaMiddleware()}. AddSendingMiddleware
// applies its arguments right-to-left (go-sdk@v1.7.0 mcp/client.go:1129), so the
// LAST middleware in this slice actually runs FIRST on the way out. That ordering
// is irrelevant here ONLY because the two middlewares write disjoint _meta.aura
// fields (operation_key/scope/fingerprint vs. user_identifier) — say so explicitly,
// because the next middleware added to this slice may not have that property, and
// silently relying on "the SDK figures out order" would be wrong for it.
func sendingMiddleware(policy bridgePolicy) []sdkmcp.Middleware {
	mw := []sdkmcp.Middleware{mcp.OperationMetaMiddleware()}
	if policy.memory {
		mw = append(mw, IdentityMetaMiddleware())
	}
	return mw
}

// mountManagedHTTPHost is the streamable-HTTP mirror of mountStdio: it opens the
// raw session and lists tools via handshakeCtx (bounded exactly like mountStdio's
// raw discovery call — same rationale, see bridgeFromAdvertised's doc comment),
// then wraps the session in a MountedServer so every CALL after a successful
// mount gets the redial-on-transport-error behavior the stdio branch already had.
func mountManagedHTTPHost(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, policy bridgePolicy, opts MountOptions) (closer func() error, names []string, host *MountedServer, err error) {
	var srv *MountedServer
	elicit := elicitationHandlerFor(name, opts.Elicitation)
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Sending = sendingMiddleware(policy)
		o.ToolListChanged = srv.onToolListChanged
		o.Elicitation = elicit
		return mcp.OpenSDKSession(hctx, name, server, opts.Egress, o)
	}
	srv = NewMountedServer(name, open)
	return openAttachAndMount(srv, processCtx, handshakeCtx, open, reg, name, policy)
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
		nil,
	)
	return closer, names, err
}

func mountStdioWithPolicyHost(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, policy bridgePolicy, consent ElicitationConsent) (closer func() error, names []string, host *MountedServer, err error) {
	var srv *MountedServer
	elicit := elicitationHandlerFor(name, consent)
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Sending = sendingMiddleware(policy)
		o.ToolListChanged = srv.onToolListChanged
		o.Elicitation = elicit
		return mcp.OpenSDKSessionForConfig(pctx, hctx, name, cfg, o)
	}
	srv = NewMountedServer(name, open)
	return openAttachAndMount(srv, processCtx, handshakeCtx, open, reg, name, policy)
}

// openAttachAndMount is the shared body both mount branches reduce to once
// their own openSessionFunc closure is built: open the first session, list its
// tools bounded purely by handshakeCtx (bridgeFromAdvertised's doc comment
// explains why that must NOT route through the supervisor), Attach, then mount
// with the given policy — reaping the session on any failure along the way.
func openAttachAndMount(srv *MountedServer, processCtx, handshakeCtx context.Context, open openSessionFunc, reg *tools.Registry, name string, policy bridgePolicy) (closer func() error, names []string, host *MountedServer, err error) {
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
