package mcptools

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// MountServer spawns one stdio MCP server, mounts its tools into reg, and returns
// a closer that shuts the subprocess down. allow is the optional per-server tool
// allowlist (nil/empty = mount all; D-20). On any failure (spawn, tools/list, name
// collision) it returns an error and leaves reg untouched / the subprocess reaped,
// so a misconfigured server can never half-register or leak a process. The closer
// MUST be called at agent shutdown (goleak-clean).
func MountServer(ctx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, allow []string) (closer func() error, names []string, err error) {
	cli, err := mcp.Open(ctx, name, cfg)
	if err != nil {
		return nil, nil, err
	}
	names, err = Mount(ctx, reg, name, cli, allow)
	if err != nil {
		_ = cli.Close()
		return nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	return cli.Close, names, nil
}

func MountServerWithPolicy(ctx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, server mcp.ManagedServer) (closer func() error, names []string, blocked []mcpmanager.PolicyDecision, err error) {
	cli, err := mcp.Open(ctx, name, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	names, blocked, err = MountWithPolicy(ctx, reg, name, cli, server)
	if err != nil {
		_ = cli.Close()
		return nil, nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	return cli.Close, names, blocked, nil
}
