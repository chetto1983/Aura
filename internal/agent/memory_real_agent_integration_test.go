//go:build memory_integration && live_memory_agent

// Paid, operator-authorized Agent Memory acceptance gate. It uses the real
// OpenRouter-backed LlmAgent, a rebuilt live sidecar, and the real Neo4j graph.
// The deterministic memory_integration test proves wiring; this tier proves that
// a provider-driven agent actually chooses and consumes the memory tool.
package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/google/uuid"
)

func requireRealMemoryAgentKey(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("live_memory_agent requires OPENROUTER_API_KEY under CI")
	}
	t.Skip("live_memory_agent is a manual paid gate; load OPENROUTER_API_KEY and rerun")
}

func TestMemoryRealAgentRecall(t *testing.T) {
	requireRealMemoryAgentKey(t)
	endpoint := memoryRecallEndpoint(t)
	reapIdleHTTPConns(t)

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	tag := "AURA-P0-REAL-AGENT-" + runID
	userID := "real-agent-user-" + runID
	subject := "Agent memory validation " + tag

	base := identityctx.WithIdentityID(context.Background(), userID)
	ctx, cancel := context.WithTimeout(base, 240*time.Second)
	defer cancel()

	server := mcp.ManagedServer{
		Type:   mcp.ServerTypeStreamableHTTP,
		URL:    endpoint,
		Source: mcp.SourceRecipeMemory,
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	}
	seed, err := mcp.OpenServer(ctx, "memory-real-agent-seed", server)
	if err != nil {
		t.Fatalf("open seed MCP at %s: %v", endpoint, err)
	}
	defer func() { _ = seed.Close() }()

	addedText, err := seed.CallTool(ctx, "memory_add_fact", map[string]any{
		"subject":         subject,
		"predicate":       "has_exact_recall_marker",
		"object_value":    tag,
		"user_identifier": userID,
		"metadata":        map[string]any{"gate": "p0-real-agent"},
	})
	if err != nil {
		t.Fatalf("seed memory_add_fact: %v", err)
	}
	var added struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(addedText), &added); err != nil || added.ID == "" {
		t.Fatalf("decode seeded fact %q: id=%q err=%v", addedText, added.ID, err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(base, 20*time.Second)
		defer cleanupCancel()
		_, _ = seed.CallTool(cleanupCtx, "memory_forget", map[string]any{
			"node_type":       "fact",
			"node_id":         added.ID,
			"user_identifier": userID,
		})
	}()

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	closer, mounted, err := mcptools.MountManagedServer(ctx, ctx, reg, "memory", server)
	if err != nil {
		t.Fatalf("mount memory MCP: %v", err)
	}
	defer func() { _ = closer() }()
	if _, ok := reg.Get("memory__memory_search"); !ok {
		t.Fatalf("memory__memory_search missing from %d mounted tools", len(mounted))
	}

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("load real LLM config: %v", err)
	}
	client := openai_compat.New(*cfg)
	maxSteps, maxWallclock := 8, 180
	budget, err := agent.NewBudget(agent.BudgetOptions{
		MaxSteps:        &maxSteps,
		MaxWallclockSec: &maxWallclock,
	})
	if err != nil {
		t.Fatalf("budget: %v", err)
	}

	prompt := fmt.Sprintf(
		"Run the Agent Memory acceptance gate. You MUST call memory__memory_search "+
			"with query %q, memory_types [\"facts\"], and threshold 0. "+
			"Do not rely on the marker shown in this instruction: verify it appears "+
			"in the tool result. Then call text_response and include the exact marker.",
		tag,
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Name:       "aura-memory-real-agent-it",
		Client:     client,
		LLM:        *cfg,
		Registry:   reg,
		PreviewCap: 1 << 20,
		RunDir:     t.TempDir(),
		SessionID:  uuid.NewString(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	ic := agent.InvocationContext{
		Ctx:       ctx,
		Agent:     a,
		RequestID: uuid.New(),
		Branch:    "root",
		Budget:    budget,
	}

	var searchStarted, searchEnded bool
	var searchPreview, finalText string
	for ev, runErr := range a.Run(ic) {
		if runErr != nil {
			t.Fatalf("real agent run: %v", runErr)
		}
		if ev == nil {
			continue
		}
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.ToolName == "memory__memory_search" {
			searchStarted = searchStarted || ti.Event == agent.ToolInvocationStart
			searchEnded = searchEnded || ti.Event == agent.ToolInvocationEnd
			if ti.Event == agent.ToolInvocationEnd {
				searchPreview = ti.ResultPreview
			}
		}
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			finalText = ev.LLMResponse.Content
		}
	}

	if !searchStarted || !searchEnded {
		t.Fatalf("real LlmAgent did not complete memory__memory_search (start=%v end=%v)",
			searchStarted, searchEnded)
	}
	if !strings.Contains(searchPreview, tag) {
		t.Fatalf("live memory tool result did not contain %q: %s", tag, searchPreview)
	}
	if !strings.Contains(finalText, tag) {
		t.Fatalf("real agent final answer did not contain recalled marker %q: %s", tag, finalText)
	}
	t.Logf("REAL AGENT E2E PASS provider=%s model=%s marker=%s", cfg.Provider, cfg.Model, tag)
}
