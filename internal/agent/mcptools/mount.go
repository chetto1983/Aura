package mcptools

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// MountServer spawns one stdio MCP server, mounts its tools into reg, and returns
// a closer that shuts the subprocess down. On any failure (spawn, tools/list, name
// collision) it returns an error and leaves reg untouched / the subprocess reaped,
// so a misconfigured server can never half-register or leak a process. The closer
// MUST be called at agent shutdown (goleak-clean).
func MountServer(ctx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig) (closer func() error, names []string, err error) {
	cli, err := mcp.Open(ctx, name, cfg)
	if err != nil {
		return nil, nil, err
	}
	names, err = Mount(ctx, reg, cli)
	if err != nil {
		_ = cli.Close()
		return nil, nil, fmt.Errorf("mount %q: %w", name, err)
	}
	return cli.Close, names, nil
}
