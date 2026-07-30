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

func TestMemoryReadinessCheckUsesFunctionalTenantScopedSearch(t *testing.T) {
	client := &memoryReadinessClient{
		text: `{"results":{"preferences":[]},"query":"Aura readiness"}`,
	}
	if err := checkMemoryReadiness(context.Background(), client); err != nil {
		t.Fatalf("checkMemoryReadiness: %v", err)
	}
	if client.name != "memory_search" {
		t.Fatalf("tool = %q, want memory_search", client.name)
	}
	if got, _ := client.args["user_identifier"].(string); got != memoryReadinessOwner {
		t.Fatalf("readiness owner = %q, want isolated synthetic owner", got)
	}
}

func TestMemoryReadinessCheckRejectsSemanticAndTransportFailure(t *testing.T) {
	for _, client := range []*memoryReadinessClient{
		{text: `{"error":"embedder unavailable"}`},
		{text: `{"results":{}}`},
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
