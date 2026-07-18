package mcptools

import (
	"context"
	"fmt"

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
// handshakeCtx bounds ONLY this mount attempt (Open's initialize round-trip AND the
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
	if isStreamableHTTPManagedServer(server) {
		srv, err := mcp.OpenServer(handshakeCtx, name, server)
		if err != nil {
			return nil, nil, err
		}
		names, err = Mount(handshakeCtx, reg, name, srv)
		if err != nil {
			_ = srv.Close()
			return nil, nil, fmt.Errorf("mount %q: %w", name, err)
		}
		return srv.Close, names, nil
	}

	cfg, err := managedStdioConfig(name, server)
	if err != nil {
		return nil, nil, err
	}
	return mountStdio(processCtx, handshakeCtx, reg, name, cfg)
}

// mountStdio is the shared stdio mount body for MountServer and MountManagedServer's
// stdio branch. It lists tools via the RAW client (cli.ListTools), NOT through
// reconnectingServer's ListTools, so the mount-time discovery call is bounded
// purely by handshakeCtx (see bridgeFromDefs's doc comment for why routing it
// through the reconnecting wrapper here would silently blow the mount deadline).
// The reconnecting wrapper is still constructed and returned as the mounted
// tools' Server, so every CALL after a successful mount gets the normal
// reconnect-on-transport-error behavior.
func mountStdio(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig) (closer func() error, names []string, err error) {
	cli, err := mcp.OpenWithHandshakeContext(processCtx, handshakeCtx, name, cfg)
	if err != nil {
		return nil, nil, err
	}
	srv := newReconnectingServer(name, cfg, cli)
	names, err = Mount(handshakeCtx, reg, name, srv)
	if err != nil {
		_ = srv.Close()
		return nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	return srv.Close, names, nil
}

// isStreamableHTTPManagedServer resolves server's transport via the single
// canonical mcp.Classify (D-01/MCPH-01), replacing the previous ad hoc
// url/type check that duplicated (and could silently drift from) Classify's own
// dispatch logic. A Classify error (an ambiguous mixed url+command entry, or an
// internally-inconsistent explicit type<->trust combination) resolves to true so
// the caller's OpenServer branch is reached instead of the stdio branch:
// OpenServer re-classifies and surfaces that SAME error, guaranteeing a
// rejected/ambiguous config can never silently fall through to a stdio subprocess
// spawn (the exact F-027 class this call-site previously risked reintroducing).
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
