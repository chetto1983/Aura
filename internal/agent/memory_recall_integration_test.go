//go:build memory_integration

// Live agent-loop recall tier (port of spike 035). Drives Aura's real LlmAgent over
// a deterministic agenttest.FakeClient scripted to call tool_search (discovering the
// deferred memory__memory_search), then memory__memory_search, then text_response —
// against the running, rebuilt agent-memory sidecar. Asserts the seeded tag survives
// the full tool_search -> memory__memory_search -> text_response path (D-03 pull-on-
// demand recall / re-scoped UX-09). This asserts the recall WIRING, not the model's
// tool-choice judgment (the FakeClient scripts the turns).
//
// No-skip-as-green (CLAUDE.md): AURA_AGENT_MEMORY_MCP_URL unset under $CI t.Fatals.
package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/google/uuid"
)

// reapIdleHTTPConns drains http.DefaultClient's idle keep-alive connections at test
// end so the live streamable-HTTP MCP transport's parked goroutines do not trip the
// package goleak TestMain. Test-only; never alters production Close() semantics.
func reapIdleHTTPConns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		time.Sleep(200 * time.Millisecond)
	})
}

func memoryRecallEndpoint(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL")); v != "" {
		return v
	}
	if port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT")); port != "" {
		return "http://127.0.0.1:" + port + "/mcp/"
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_AGENT_MEMORY_MCP_URL (or _PORT) must be set under CI — a skipped memory_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
	}
	t.Skip("set AURA_AGENT_MEMORY_MCP_URL (or _PORT) + bring the stack up to run the memory loop-recall tier")
	return ""
}

func intPtr(v int) *int { return &v }

// TestMemoryLoopRecall seeds a uniquely-tagged memory, mounts the live memory MCP
// server (the default-on recipe's surface), and runs the real LlmAgent driven by a
// scripted FakeClient through tool_search -> memory__memory_search -> text_response.
// The final text must contain the seeded tag (the spike-035 path under project gates).
func TestMemoryLoopRecall(t *testing.T) {
	endpoint := memoryRecallEndpoint(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	tag := fmt.Sprintf("AURA-P15-IT-recall-%d", time.Now().UnixNano())
	sessionID := strings.ToLower(tag) + "-session"
	message := tag + " The agent loop must recall this memory via the live MCP bridge."

	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	}

	// Seed the tagged memory through the live sidecar.
	seed, err := mcp.OpenServer(ctx, "memory-recall-seed", server)
	if err != nil {
		t.Fatalf("open seed MCP at %s: %v", endpoint, err)
	}
	if _, err := seed.CallTool(ctx, "memory_store_message", map[string]any{
		"content":    message,
		"role":       "user",
		"session_id": sessionID,
		"metadata":   map[string]any{"phase": "15", "tag": tag},
	}); err != nil {
		_ = seed.Close()
		t.Fatalf("seed memory_store_message: %v", err)
	}
	_ = seed.Close()

	// Build the registry the way the loop does: the discovery tools + the live
	// memory MCP surface mounted Deferred/namespaced via the real bridge.
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	closer, mounted, err := mcptools.MountManagedServer(ctx, reg, "memory", server)
	if err != nil {
		t.Fatalf("mount memory MCP: %v", err)
	}
	defer func() { _ = closer() }()
	if len(mounted) == 0 {
		t.Fatalf("mounted 0 memory tools — the live surface must expose memory__memory_search")
	}

	searchArgs := fmt.Sprintf(`{"query":%q,"limit":5,"memory_types":["messages"],"session_id":%q,"threshold":0}`, tag, sessionID)
	fake := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call_search_spec", "tool_search", `{"query":"select:memory__memory_search"}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call_memory_search", "memory__memory_search", searchArgs)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call_final", "text_response", fmt.Sprintf(`{"text":"recalled %s via memory MCP"}`, tag))),
	)

	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: intPtr(6), MaxWallclockSec: intPtr(90)})
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuidv7: %v", err)
	}
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Name:       "aura-memory-loop-it",
		Client:     fake,
		LLM:        llm.Config{Model: "fake-memory-loop", Provider: "fake", TotalTimeoutSec: 30, MaxTokens: 512},
		Registry:   reg,
		PreviewCap: 1 << 20,
		RunDir:     t.TempDir(),
		SessionID:  sessionID,
		UserTurns: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "Recall the tagged memory " + tag + " using memory tools, then answer with text_response.",
		}},
	})
	ic := agent.InvocationContext{
		Ctx:       ctx,
		Agent:     a,
		RequestID: requestID,
		Branch:    "root",
		Budget:    budget,
	}

	var invocations []string
	var finalText, memoryPreview string
	for ev, runErr := range a.Run(ic) {
		if runErr != nil {
			t.Fatalf("agent run: %v", runErr)
		}
		if ev == nil {
			continue
		}
		if ti := ev.Actions.ToolInvocation; ti != nil {
			invocations = append(invocations, ti.Event+":"+ti.ToolName)
			if ti.Event == agent.ToolInvocationEnd && ti.ToolName == "memory__memory_search" {
				memoryPreview = ti.ResultPreview
			}
		}
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			finalText = ev.LLMResponse.Content
		}
	}

	if !contains(invocations, "start:tool_search") || !contains(invocations, "end:tool_search") {
		t.Errorf("loop did not invoke tool_search; invocations=%v", invocations)
	}
	if !contains(invocations, "start:memory__memory_search") || !contains(invocations, "end:memory__memory_search") {
		t.Errorf("loop did not dispatch memory__memory_search; invocations=%v", invocations)
	}
	if !strings.Contains(memoryPreview, tag) {
		t.Errorf("memory__memory_search preview did not contain the seeded tag %q; preview=%q", tag, memoryPreview)
	}
	if !strings.Contains(finalText, tag) {
		t.Fatalf("final text_response did not carry the recalled tag %q; final=%q", tag, finalText)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
