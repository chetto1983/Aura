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

// MountManagedServerHostWithEgress is MountManagedServerWithEgress plus the
// process-owned host view used by trusted daemon integrations such as automatic
// Memory recall and readiness.
func MountManagedServerHostWithEgress(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, egress mcp.EgressPolicy) (closer func() error, names []string, host *MountedServer, err error) {
	policy := managedBridgePolicy(server)
	if isStreamableHTTPManagedServer(server) {
		return mountManagedHTTPHost(processCtx, handshakeCtx, reg, name, server, policy, egress)
	}

	cfg, err := managedStdioConfig(name, server)
	if err != nil {
		return nil, nil, nil, err
	}
	return mountStdioWithPolicyHost(processCtx, handshakeCtx, reg, name, cfg, policy)
}

// sendingMiddleware is the sending middleware stack every Aura MCP session opens
// with: the _meta.aura operation stamp (Task 1). It is the same slice for both
// mount branches so a policy fix in one cannot miss the other.
func sendingMiddleware() []sdkmcp.Middleware {
	return []sdkmcp.Middleware{mcp.OperationMetaMiddleware()}
}

// mountManagedHTTPHost is the streamable-HTTP mirror of mountStdio: it opens the
// raw session and lists tools via handshakeCtx (bounded exactly like mountStdio's
// raw discovery call — same rationale, see bridgeFromAdvertised's doc comment),
// then wraps the session in a MountedServer so every CALL after a successful
// mount gets the redial-on-transport-error behavior the stdio branch already had.
func mountManagedHTTPHost(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer, policy bridgePolicy, egress mcp.EgressPolicy) (closer func() error, names []string, host *MountedServer, err error) {
	var srv *MountedServer
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Sending = sendingMiddleware()
		o.ToolListChanged = srv.onToolListChanged
		return mcp.OpenSDKSession(hctx, name, server, egress, o)
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
	)
	return closer, names, err
}

func mountStdioWithPolicyHost(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, policy bridgePolicy) (closer func() error, names []string, host *MountedServer, err error) {
	var srv *MountedServer
	open := func(pctx, hctx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		o.Sending = sendingMiddleware()
		o.ToolListChanged = srv.onToolListChanged
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
	srv.setProcessContext(processCtx)

	session, err := open(processCtx, handshakeCtx, mcp.SessionOptions{})
	if err != nil {
		return nil, nil, nil, err
	}
	advertised, err := drainTools(handshakeCtx, session)
	if err != nil {
		_ = session.Close()
		return nil, nil, nil, err
	}
	srv.Attach(session)

	names, err = mountWithAdvertisedPolicy(reg, name, srv, advertised, policy)
	if err != nil {
		_ = srv.Close()
		return nil, nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	srv.trackAcceptedTools(advertised)
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
