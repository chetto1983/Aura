package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestMCPMountOptionsCarryTheViewsAndTheFileSink(t *testing.T) {
	handles := &runtimeToolHandles{MCPViews: mcp.NewViewCatalog(), MCPFiles: &tools.MCPFileSink{}}

	opts := mcpMountOptions(context.Background(), true, mcp.ManagedServer{URL: "https://mcp.example/mcp"}, handles)

	if opts.Views != handles.MCPViews || opts.Files != handles.MCPFiles {
		t.Fatalf("mount options = %+v, want the handles' views and file sink", opts)
	}
}

func TestBuildRegistryWithMCP_HandsMountsTheBoxFileSink(t *testing.T) {
	withMemoryMCPRegistry(t)
	router := usersandbox.NewSandboxRouter(nil, config.ProfileSingleUserHardened, config.SandboxConfig{})

	_, handles, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, router, nil)
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	defer func() { _ = closeMCPServers(closers) }()

	requireFileSinkOver(t, handles, router)
}

// A server mounted live (an operator's install, a completed authorization) reads its sink
// off the boot handles, so the sink must exist even when boot mounted nothing and left
// buildRegistryWithMCP through its empty-set early return.
func TestBuildRegistryWithMCP_NoBootServersStillHandsLiveMountsTheFileSink(t *testing.T) {
	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, withDefaultOnRecipesOff(mcp.ManagedConfig{}))
	router := usersandbox.NewSandboxRouter(nil, config.ProfileSingleUserHardened, config.SandboxConfig{})

	_, handles, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, router, nil)
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	defer func() { _ = closeMCPServers(closers) }()

	if len(closers) != 0 {
		t.Fatalf("boot mounted %d servers, want none: the early return is what this test pins", len(closers))
	}
	requireFileSinkOver(t, handles, router)
}

func requireFileSinkOver(t *testing.T, handles runtimeToolHandles, router *usersandbox.SandboxRouter) {
	t.Helper()
	sink, ok := handles.MCPFiles.(*tools.MCPFileSink)
	if !ok || sink.Router != router {
		t.Fatalf("MCPFiles = %#v, want an MCPFileSink over the boot router", handles.MCPFiles)
	}
}
