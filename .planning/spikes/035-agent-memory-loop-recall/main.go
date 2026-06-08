package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/google/uuid"
)

type summary struct {
	Spike                     string   `json:"spike"`
	Endpoint                  string   `json:"endpoint"`
	SessionID                 string   `json:"session_id"`
	Tag                       string   `json:"tag"`
	FirstRequestDeferred      bool     `json:"first_request_deferred"`
	ToolInvocations           []string `json:"tool_invocations"`
	MemorySearchPreviewHasTag bool     `json:"memory_search_preview_has_tag"`
	FinalText                 string   `json:"final_text"`
	Verdict                   string   `json:"verdict"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoint := memoryEndpoint()
	tag := fmt.Sprintf("AURA-SPIKE-035-%d", time.Now().Unix())
	sessionID := strings.ToLower(tag) + "-session"
	message := tag + " The deterministic agent loop must recall this memory via MCP."
	logf("SETUP", "endpoint=%s session=%s tag=%s", endpoint, sessionID, tag)

	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}

	seed, err := mcp.OpenServer(ctx, "agent-memory-seed", server)
	if err != nil {
		failf("open seed MCP: %v", err)
	}
	if _, err := seed.CallTool(ctx, "memory_store_message", map[string]any{
		"content":    message,
		"role":       "user",
		"session_id": sessionID,
		"metadata":   map[string]any{"spike": "035", "tag": tag},
	}); err != nil {
		_ = seed.Close()
		failf("seed memory_store_message: %v", err)
	}
	_ = seed.Close()
	logf("SEED", "stored tagged message")

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	closer, mounted, blocked, err := mcptools.MountManagedServerWithPolicy(ctx, reg, "memory", server)
	if err != nil {
		failf("mount memory MCP: %v", err)
	}
	defer func() { _ = closer() }()
	logf("MOUNT", "mounted=%d blocked=%d", len(mounted), len(blocked))

	searchArgs := fmt.Sprintf(`{"query":%q,"limit":5,"memory_types":["messages"],"session_id":%q,"threshold":0}`, tag, sessionID)
	fake := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call_search_spec", "tool_search", `{"query":"select:memory__memory_search"}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call_memory_search", "memory__memory_search", searchArgs)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call_final", "text_response", fmt.Sprintf(`{"text":"recalled %s via memory MCP"}`, tag))),
	)

	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: intPtr(6), MaxWallclockSec: intPtr(90)})
	if err != nil {
		failf("budget: %v", err)
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		failf("uuidv7: %v", err)
	}
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Name:       "aura-memory-loop-spike",
		Client:     fake,
		LLM:        llm.Config{Model: "fake-memory-loop", Provider: "fake", TotalTimeoutSec: 30, MaxTokens: 512},
		Registry:   reg,
		PreviewCap: 1 << 20,
		RunDir:     os.TempDir(),
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
	var finalText string
	var memoryPreview string
	for ev, runErr := range a.Run(ic) {
		if runErr != nil {
			failf("agent run: %v", runErr)
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

	firstDeferred := false
	if len(fake.Requests) > 0 {
		firstDeferred = firstRequestHasDeferredStub(fake.Requests[0], "memory__memory_search")
	}
	hasStartSearch := contains(invocations, "start:memory__memory_search")
	hasEndSearch := contains(invocations, "end:memory__memory_search")
	hasToolSearch := contains(invocations, "start:tool_search") && contains(invocations, "end:tool_search")
	previewHasTag := strings.Contains(memoryPreview, tag)
	finalHasTag := strings.Contains(finalText, tag)

	verdict := "VALIDATED"
	if !firstDeferred || !hasToolSearch || !hasStartSearch || !hasEndSearch || !previewHasTag || !finalHasTag {
		verdict = "INVALIDATED"
	}
	out := summary{
		Spike:                     "035-agent-memory-loop-recall",
		Endpoint:                  endpoint,
		SessionID:                 sessionID,
		Tag:                       tag,
		FirstRequestDeferred:      firstDeferred,
		ToolInvocations:           invocations,
		MemorySearchPreviewHasTag: previewHasTag,
		FinalText:                 finalText,
		Verdict:                   verdict,
	}
	printSummary(out)
	if verdict != "VALIDATED" {
		os.Exit(1)
	}
}

func firstRequestHasDeferredStub(req llm.Request, name string) bool {
	for _, def := range req.Tools {
		if def.Function.Name == name {
			return len(def.Function.Parameters) == 0 && strings.Contains(def.Function.Description, "Required args:")
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func intPtr(v int) *int { return &v }

func memoryEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL")); v != "" {
		return v
	}
	port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT"))
	if port == "" {
		port = "8091"
	}
	return "http://127.0.0.1:" + port + "/mcp/"
}

func printSummary(out summary) {
	sort.Strings(out.ToolInvocations)
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
	logf("SUMMARY", "verdict=%s final=%q", out.Verdict, out.FinalText)
}

func logf(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}

func failf(format string, args ...any) {
	logf("ERROR", format, args...)
	os.Exit(1)
}
