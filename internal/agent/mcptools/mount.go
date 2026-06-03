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

// MountCodeSandbox auto-provisions the pinned code-sandbox-mcp binary (downloading
// it into cacheDir if absent — empty cacheDir uses ~/.aura/bin) and mounts its
// tools (sandbox_initialize/sandbox_exec/…) into reg. It is spawned with -no-update
// so it never mutates itself. This is the zero-operator-install boot path for
// Aura's sandbox capability (CAP-01/CAP-02). Returns the shutdown closer + the
// registered tool names.
func MountCodeSandbox(ctx context.Context, reg *tools.Registry, cacheDir string) (func() error, []string, error) {
	bin, err := mcp.EnsureCodeSandboxBinary(ctx, cacheDir)
	if err != nil {
		return nil, nil, fmt.Errorf("provision code-sandbox-mcp: %w", err)
	}
	return MountServer(ctx, reg, "code-sandbox", mcp.ServerConfig{
		Command: bin,
		Args:    []string{"-no-update"},
	})
}
