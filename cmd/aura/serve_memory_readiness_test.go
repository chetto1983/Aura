package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/readiness"
)

type memoryReadinessClient struct {
	text string
	err  error
	name string
	args map[string]any
}

func (*memoryReadinessClient) ListTools(context.Context) ([]mcp.ToolDef, error) {
	return nil, nil
}

func (c *memoryReadinessClient) CallTool(
	_ context.Context,
	name string,
	args map[string]any,
) (string, error) {
	c.name = name
	c.args = args
	return c.text, c.err
}

func (*memoryReadinessClient) Ping(context.Context) error { return nil }

// The readiness search uses a synthetic owner whose isolated ArcadeDB database
// cannot expose or disturb a person's memory.
func TestMemoryReadinessCheckRunsAnIsolatedFunctionalSearch(t *testing.T) {
	client := &memoryReadinessClient{text: `{"facts":[]}`}
	if err := checkMemoryReadiness(context.Background(), client); err != nil {
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
		if err := checkMemoryReadiness(context.Background(), client); err == nil {
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
