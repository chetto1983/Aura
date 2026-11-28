package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// The defect these pin: the dispatch gate refuses a deferred tool called before its schema
// was read, hands the schema back and says the tool "is now loaded". Within the turn that
// held. On the next turn the agent is rebuilt from the stored history, and the loaded set
// was re-derived from tool_search results alone, so the gate's own refusal no longer
// counted. The tool stayed callable only while it was among the last
// maxPromotedDeferredTools calls of the conversation, through `activated`; past that, the
// model was refused a tool it had been told was loaded, and spent a round trip on it.
//
// The reload here is rebuilt by storeShapedHistory from the turn's events, the way
// runner_persist.go writes them; it is not the real AppendTurn -> LoadHistory round trip.

// gateReloadFillers is one more than the manifest cap, so the tool the model loaded falls
// out of the recently-called window that keeps it in `activated`.
const gateReloadFillers = maxPromotedDeferredTools + 1

// refusalPrefix pins the bytes the gate's refusal opens with on every route.
const refusalPrefix = "error: tool_not_loaded: "

// alphaDescription is markdown with headings, as MCP tool descriptions routinely are: a
// grant rule that reads "## " lines as spec headers loses the tool on it.
const alphaDescription = "Does alpha.\n## Usage\nCall it with no arguments.\n## Notes\nParameters: none."

func gateReloadRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(decayTool{name: "alpha", deferred: true, description: alphaDescription})
	for i := range gateReloadFillers {
		reg.Register(decayTool{name: fmt.Sprintf("filler_%02d", i)})
	}
	return reg
}

func gateReloadFillerCalls() []llm.ToolCall {
	calls := make([]llm.ToolCall, 0, gateReloadFillers)
	for i := range gateReloadFillers {
		calls = append(calls, modelRoundToolCall(fmt.Sprintf("fill-%02d", i), fmt.Sprintf("filler_%02d", i), `{}`))
	}
	return calls
}

func gateReloadRoutes() map[string]llm.Config {
	return map[string]llm.Config{
		"openrouter": openrouterRouteConfig(),
		"anthropic":  anthropicRouteConfig(),
		"chatgpt":    {Model: "gpt-test", Provider: llm.ChatGPTProvider, TotalTimeoutSec: 30, MaxTokens: 4096},
		"llamacpp":   {Model: "local", Provider: "llamacpp", BaseURL: "http://127.0.0.1:8080/v1", TotalTimeoutSec: 30, MaxTokens: 4096},
	}
}

// storeShapedHistory rebuilds a turn the way runner_persist.go writes it and LoadHistory
// reads it back: the user message, one assistant message per tool batch carrying the
// calls, one RoleTool message per result holding the raw preview, and the final answer.
func storeShapedHistory(user string, events []*Event) []llm.Message {
	hist := []llm.Message{{Role: llm.RoleUser, Content: user}}
	var pending []llm.ToolCall
	for _, ev := range events {
		if ti := ev.Actions.ToolInvocation; ti != nil {
			switch ti.Event {
			case ToolInvocationStart:
				pending = append(pending, modelRoundToolCall(ti.ToolCallID, ti.ToolName, ti.Arguments))
			case ToolInvocationEnd:
				if len(pending) > 0 {
					hist = append(hist, llm.Message{Role: llm.RoleAssistant, ToolCalls: pending})
					pending = nil
				}
				hist = append(hist, llm.Message{Role: llm.RoleTool, ToolCallID: ti.ToolCallID, Content: ti.ResultPreview})
			}
			continue
		}
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			hist = append(hist, llm.Message{Role: llm.RoleAssistant, Content: ev.LLMResponse.Content})
		}
	}
	return hist
}

// runGateReloadTurn drives one turn of the real agent and returns its events and requests.
func runGateReloadTurn(t *testing.T, cfg llm.Config, reg *tools.Registry, hist []llm.Message, script []modelRoundTurn) ([]*Event, []llm.Request) {
	t.Helper()
	client := &routeClient{turns: script}
	a := NewLlmAgent(LlmAgentConfig{
		Client: client, LLM: cfg, Registry: reg, PreviewCap: 8192, RunDir: t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(), UserTurns: hist,
	})
	maxSteps := 20
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	var events []*Event
	ic := InvocationContext{Ctx: context.Background(), Agent: a, RequestID: uuid.Must(uuid.NewV7()), Budget: budget}
	for ev, err := range a.Run(ic) {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		events = append(events, ev)
	}
	return events, client.requests
}

func toolResultPreview(t *testing.T, events []*Event, callID string) string {
	t.Helper()
	for _, ev := range events {
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == ToolInvocationEnd && ti.ToolCallID == callID {
			return ti.ResultPreview
		}
	}
	t.Fatalf("call %s was never dispatched", callID)
	return ""
}

type gateReloadOutcome struct {
	loaded     string        // turn 1's result of the call that loaded alpha
	again      string        // turn 2's result of calling alpha again
	turn2Tools []llm.ToolDef // the manifest of turn 2's first request
}

// gateReloadScenario runs turn 1 (load alpha, call it, and with fillers push it out of the
// recently-called window), reloads the history the way the store returns it, and calls
// alpha again in turn 2.
func gateReloadScenario(t *testing.T, cfg llm.Config, load llm.ToolCall, fillers bool) gateReloadOutcome {
	t.Helper()
	reg := gateReloadRegistry()
	const user1 = "usa alpha"
	turn1 := []modelRoundTurn{
		modelRoundToolTurn(load),
		modelRoundToolTurn(modelRoundToolCall("run-alpha", "alpha", `{}`)),
	}
	if fillers {
		turn1 = append(turn1, modelRoundToolTurn(gateReloadFillerCalls()...))
	}
	events1, _ := runGateReloadTurn(t, cfg, reg, []llm.Message{{Role: llm.RoleUser, Content: user1}}, turn1)
	if got := toolResultPreview(t, events1, "run-alpha"); strings.HasPrefix(got, refusalPrefix) {
		t.Fatalf("turn 1: alpha was refused after being loaded: %q", got)
	}

	hist := append(storeShapedHistory(user1, events1), llm.Message{Role: llm.RoleUser, Content: "usala di nuovo"})
	events2, requests2 := runGateReloadTurn(t, cfg, reg, hist, []modelRoundTurn{
		modelRoundToolTurn(modelRoundToolCall("again-alpha", "alpha", `{}`)),
	})
	return gateReloadOutcome{
		loaded:     toolResultPreview(t, events1, load.ID),
		again:      toolResultPreview(t, events2, "again-alpha"),
		turn2Tools: requests2[0].Tools,
	}
}

// forEachGateReloadCase runs body on every route, with alpha still among the recent calls
// and with it pushed out of that window.
func forEachGateReloadCase(t *testing.T, body func(t *testing.T, cfg llm.Config, fillers bool)) {
	t.Helper()
	for name, cfg := range gateReloadRoutes() {
		for _, fillers := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pushed_out=%t", name, fillers), func(t *testing.T) {
				t.Parallel()
				body(t, cfg, fillers)
			})
		}
	}
}

func TestGateLoadedToolStaysLoadedAfterReload(t *testing.T) {
	t.Parallel()
	forEachGateReloadCase(t, func(t *testing.T, cfg llm.Config, fillers bool) {
		got := gateReloadScenario(t, cfg, modelRoundToolCall("cold-alpha", "alpha", `{"invented":true}`), fillers)
		if !strings.HasPrefix(got.loaded, refusalPrefix) {
			t.Fatalf("precondition: the gate did not refuse the cold call to alpha: %q", got.loaded)
		}
		if strings.HasPrefix(got.again, refusalPrefix) {
			t.Fatalf("turn 2 refused alpha, which the gate loaded in turn 1 and whose schema is still in the history:\n%s", got.again)
		}
		// Only the gate learns the refusal: the manifest still forgets a tool past the cap,
		// so no route's request bytes change.
		if fillers && toolPresent(got.turn2Tools, "alpha") {
			t.Error("alpha is back in the manifest: the fix must widen the gate, not the request")
		}
	})
}

// The control: the same conversation with alpha loaded through tool_search. It passed
// before the fix, and it is what the gate-loaded case has to match.
func TestSearchLoadedToolStaysLoadedAfterReload(t *testing.T) {
	t.Parallel()
	forEachGateReloadCase(t, func(t *testing.T, cfg llm.Config, fillers bool) {
		got := gateReloadScenario(t, cfg, modelRoundToolCall("search-alpha", "tool_search", `{"query":"select:alpha"}`), fillers)
		if !strings.Contains(got.loaded, "## alpha") {
			t.Fatalf("precondition: tool_search did not load alpha: %q", got.loaded)
		}
		if strings.HasPrefix(got.again, refusalPrefix) {
			t.Fatalf("turn 2 refused alpha, loaded by tool_search in turn 1:\n%s", got.again)
		}
	})
}

// The live set and the reloaded one must be the same function of the history: after a
// gate refusal, the agent that ran it and the derivation over its history agree.
//
// It compares LIVE bytes only (a.history), not the store's. The two differ for an
// untrusted tool: live holds its nonce envelope, the store its raw preview. A raw preview
// can begin with a refusal of the very tool that produced it, which would grant that tool
// on reload alone. That is harmless: the tool had already run, and the grant names only it
// and requires its exact current spec.
func TestLiveLoadedSetEqualsTheReloadedOne(t *testing.T) {
	t.Parallel()
	reg := gateReloadRegistry()
	client := &routeClient{turns: []modelRoundTurn{
		modelRoundToolTurn(modelRoundToolCall("cold-alpha", "alpha", `{}`)),
	}}
	a := NewLlmAgent(LlmAgentConfig{
		Client: client, LLM: openrouterRouteConfig(), Registry: reg, PreviewCap: 8192, RunDir: t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(), UserTurns: []llm.Message{{Role: llm.RoleUser, Content: "usa alpha"}},
	})
	maxSteps := 10
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	for _, err := range a.Run(InvocationContext{Ctx: context.Background(), Agent: a, RequestID: uuid.Must(uuid.NewV7()), Budget: budget}) {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	reloaded := deriveEverLoaded(a.history, reg)
	if len(a.everLoaded) != 1 || len(reloaded) != 1 {
		t.Fatalf("live everLoaded = %v, reloaded = %v, want both exactly {alpha}", a.everLoaded, reloaded)
	}
	if _, ok := reloaded["alpha"]; !ok {
		t.Fatalf("reloaded = %v, want {alpha}", reloaded)
	}
	if _, ok := a.everLoaded["alpha"]; !ok {
		t.Fatalf("live everLoaded = %v, want {alpha}", a.everLoaded)
	}
}
