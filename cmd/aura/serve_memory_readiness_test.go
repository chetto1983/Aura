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
}

func (c *memoryReadinessClient) mount(t *testing.T) *mcptools.MountedServer {
	t.Helper()
	handler := func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		c.mu.Lock()
		c.name = req.Params.Name
		c.args = args
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
	sdkClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "aura-test", Version: "0.0.1"}, nil)
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
	if err := checkMemoryReadiness(context.Background(), client.mount(t)); err != nil {
		t.Fatalf("checkMemoryReadiness: %v", err)
	}
	if client.name != "memory_search" {
		t.Fatalf("tool = %q, want memory_search", client.name)
	}
	// An EMPTY memory is ready. Requiring a hit would make a fresh install
	// permanently unready.
	// The isolation is back, and stronger than it was: memory is one database per
	// identity, so the synthetic owner has its own and readiness cannot read or
	// disturb a real person's memory.
	if got, _ := client.args["user_identifier"].(string); got != memoryReadinessOwner {
		t.Fatalf("readiness owner = %q, want the isolated synthetic owner", got)
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
		if err := checkMemoryReadiness(context.Background(), client.mount(t)); err == nil {
			t.Fatalf("checkMemoryReadiness(%+v) returned nil", client)
		}
	}
}

func TestMemoryReadinessProbeFailsWhenRequiredMountIsMissing(t *testing.T) {
	chat := &chatEnv{cfg: &config.Config{
		MCPPolicies: map[string]mcp.ManagedServer{
			"alias": {Source: mcp.SourceRecipeMemory},
		},
	}}
	probe, required := memoryReadinessProbe(chat)
	if !required || probe.Code != readiness.CodeMemoryUnavailable {
		t.Fatalf("probe required/code = %v/%q", required, probe.Code)
	}
	if err := probe.Check(context.Background()); err == nil {
		t.Fatal("required but missing memory mount reported ready")
	}
}
