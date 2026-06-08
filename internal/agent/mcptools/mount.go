package mcptools

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// MountServer spawns one stdio MCP server, mounts all advertised tools into reg,
// and returns a closer that shuts the subprocess down. On any failure (spawn,
// tools/list, name collision) it returns an error and leaves reg untouched / the
// subprocess reaped, so a misconfigured server can never half-register or leak a
// process. The closer MUST be called at agent shutdown (goleak-clean).
func MountServer(ctx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig) (closer func() error, names []string, err error) {
	cli, err := mcp.Open(ctx, name, cfg)
	if err != nil {
		return nil, nil, err
	}
	names, err = Mount(ctx, reg, name, cli)
	if err != nil {
		_ = cli.Close()
		return nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	return cli.Close, names, nil
}

// MountManagedServer opens a managed MCP server (stdio or streamable HTTP,
// resolved from its config), mounts all advertised tools into reg, and returns a
// closer for the underlying transport.
func MountManagedServer(ctx context.Context, reg *tools.Registry, name string, server mcp.ManagedServer) (closer func() error, names []string, err error) {
	srv, err := openManagedServer(ctx, name, server)
	if err != nil {
		return nil, nil, err
	}
	names, err = Mount(ctx, reg, name, srv)
	if err != nil {
		_ = srv.Close()
		return nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	return srv.Close, names, nil
}

func openManagedServer(ctx context.Context, name string, server mcp.ManagedServer) (mcp.Transport, error) {
	if strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
		return mcp.OpenServer(ctx, name, server)
	}
	cfg, err := mcpmanager.RuntimeLaunchConfig(name, server)
	if err != nil {
		return nil, err
	}
	return mcp.Open(ctx, name, cfg)
}
