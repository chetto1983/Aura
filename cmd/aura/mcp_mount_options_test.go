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

	sink, ok := handles.MCPFiles.(*tools.MCPFileSink)
	if !ok || sink.Router != router {
		t.Fatalf("MCPFiles = %#v, want an MCPFileSink over the boot router", handles.MCPFiles)
	}
}
