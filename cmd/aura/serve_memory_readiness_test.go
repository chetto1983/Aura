package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/readiness"
)

// memoryReadinessClient is a scripted fixture: its mount(t) method builds a real
// *mcptools.MountedServer over an in-memory SDK session (mcptools.HostClient is
// gone — MountedServer is the concrete type now, so a hand-rolled double can no
// longer satisfy anything). Both call sites this package uses ("memory_search",
// "memory_digest") route to the SAME handler, which always answers with
// text/err and records the call it observed.
type memoryReadinessClient struct {
	text string
	err  error

	mu   sync.Mutex
	name string
	args map[string]any
	meta map[string]any
}

func (c *memoryReadinessClient) mount(t *testing.T, owner string) *mcptools.MountedServer {
	t.Helper()
	handler := func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		c.mu.Lock()
		c.name = req.Params.Name
		c.args = args
		c.meta = req.Params.GetMeta()
		c.mu.Unlock()
		if c.err != nil {
			return nil, c.err
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: c.text}}}, nil
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture-memory", Version: "0.0.1"}, nil)
	for _, name := range []string{"memory_search", "memory_digest"} {
		server.AddTool(&sdkmcp.Tool{Name: name, Description: "fixture", InputSchema: map[string]any{"type": "object"}}, handler)
	}

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	mounted := mcptools.NewMountedServer("fixture-memory", func(context.Context, context.Context, mcp.SessionOptions) (*sdkmcp.ClientSession, error) {
		return nil, errors.New("redial not exercised by this fixture")
	})
	// The production pool binds each OAuth session to one identity. This fixture
	// exercises the same per-call ownership check without inventing wire metadata.
	sdkClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "aura-test", Version: "0.0.1"}, mcp.SDKClientOptions(mcp.SessionOptions{}))
	sdkClient.AddSendingMiddleware(mcptools.IdentityBindingMiddleware(owner))
	session, err := sdkClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	mounted.Attach(session)
	return mounted
}

// The readiness search uses a synthetic owner whose isolated ArcadeDB database
// cannot expose or disturb a person's memory.
func TestMemoryReadinessCheckRunsAnIsolatedFunctionalSearch(t *testing.T) {
	client := &memoryReadinessClient{text: `{"facts":[]}`}
	if err := checkMemoryReadiness(context.Background(), client.mount(t, memoryReadinessOwner), memoryReadinessOwner); err != nil {
		t.Fatalf("checkMemoryReadiness: %v", err)
	}
	if client.name != "memory_search" {
		t.Fatalf("tool = %q, want memory_search", client.name)
	}
	// An EMPTY memory is ready. Requiring a hit would make a fresh install
	// permanently unready.
	// The isolation is back, and stronger than it was: memory is one database per
	// identity, so the synthetic owner has its own and readiness cannot read or
	// disturb a real person's memory. The binding above proves the expected owner
	// was present on the call context, while the server sees no Aura metadata.
	if aura, present := client.meta["aura"]; present {
		t.Fatalf("readiness sent proprietary Aura metadata: %v", aura)
	}
	if _, present := client.args["user_identifier"]; present {
		t.Error("readiness owner leaked into wire arguments")
	}
	// memory_types belonged to the MCP this replaced; the tool rejects unknown
	// properties, so its return would fail every probe.
	if _, present := client.args["memory_types"]; present {
		t.Error("probe still sends memory_types, which the mounted tool rejects")
	}
}

func TestMemoryReadinessCheckRejectsSemanticAndTransportFailure(t *testing.T) {
	for _, client := range []*memoryReadinessClient{
		{text: `{"error":"embedder unavailable"}`},
		{text: `{"results":{}}`}, // the previous MCP's shape: no facts array at all
		{text: `not json`},
		{err: errors.New("transport unavailable")},
	} {
		if err := checkMemoryReadiness(context.Background(), client.mount(t, memoryReadinessOwner), memoryReadinessOwner); err == nil {
			t.Fatalf("checkMemoryReadiness(%+v) returned nil", client)
		}
	}
}

func TestMemoryReadinessProbeFailsWhenRequiredMountIsMissing(t *testing.T) {
	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"alias": {Command: "memory-bin", Source: mcp.SourceRecipeMemory},
	}})
	chat := &chatEnv{cfg: config.LoadDB()}
	probe, required := memoryReadinessProbe(chat)
	if !required || probe.Code != readiness.CodeMemoryUnavailable {
		t.Fatalf("probe required/code = %v/%q", required, probe.Code)
	}
	if err := probe.Check(context.Background()); err == nil {
		t.Fatal("required but missing memory mount reported ready")
	}
}

func TestMemoryReadinessProbeUsesTheAuthorizedLiveMountOwner(t *testing.T) {
	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"alias": {Command: "memory-bin", Source: mcp.SourceRecipeMemory},
	}})
	const owner = "authorized-memory-owner"
	client := (&memoryReadinessClient{text: `{"facts":[]}`}).mount(t, owner)
	chat := &chatEnv{
		cfg: config.LoadDB(),
		liveMCP: &liveMCPMount{
			hosts:  map[string]*mcptools.MountedServer{"alias": client},
			owners: map[string]string{"alias": owner},
		},
	}

	probe, required := memoryReadinessProbe(chat)
	if !required {
		t.Fatal("memory readiness probe is not required")
	}
	if err := probe.Check(context.Background()); err != nil {
		t.Fatalf("readiness did not reuse the live mount grant owner: %v", err)
	}
}
