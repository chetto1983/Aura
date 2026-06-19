package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/web"
	"github.com/google/uuid"
)

// fakeSearchEngine is a deterministic web.Client.Search stand-in for the source-list
// tail-inject test (no network).
type fakeSearchEngine struct{ results []web.Result }

func (f fakeSearchEngine) Search(_ context.Context, _ web.SearchParams) ([]web.Result, error) {
	return f.results, nil
}

func webSearchCall(id, query string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"query": query})
	return agenttest.MakeToolCall(id, "web_search", string(args))
}

func newSourcesAgent(t *testing.T, fc *agenttest.FakeClient, results []web.Result) *agent.LlmAgent {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.WebSearch{Engine: fakeSearchEngine{results: results}})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 4096,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "what's the weather"}},
	})
}

// TestSourcesTailInjectedAfterWebSearch (DISP-01/05): a web_search turn accumulates
// its hits into the per-run registry; the NEXT LLM request carries the numbered
// `[n] Title — url` list in the tail-injected hint, while messages[0] stays
// byte-identical to the first request (the volatile list never poisons the cached
// prefix).
func TestSourcesTailInjectedAfterWebSearch(t *testing.T) {
	recordingProvider(t)
	results := []web.Result{
		{Title: "Rome forecast", URL: "https://weather.test/rome", Snippet: "sunny"},
		{Title: "Milan forecast", URL: "https://weather.test/milan", Snippet: "rain"},
	}
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(webSearchCall("c1", "weather rome")),
		agenttest.ToolCallTurn(textResponseCall("c2", "Rome is sunny [1]; Milan is rainy [2].")),
	)
	a := newSourcesAgent(t, fc, results)
	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fc.CallCount() != 2 {
		t.Fatalf("expected 2 LLM calls (search turn + final), got %d", fc.CallCount())
	}

	first := fc.Requests[0]
	last := fc.LastRequest()

	// messages[0] is byte-identical across both requests (cache invariant).
	h0First, err := prompt.PrefixHash(first.Messages, []int{0})
	if err != nil {
		t.Fatalf("hash first messages[0]: %v", err)
	}
	h0Last, err := prompt.PrefixHash(last.Messages, []int{0})
	if err != nil {
		t.Fatalf("hash last messages[0]: %v", err)
	}
	if h0First != h0Last {
		t.Fatalf("messages[0] drifted across the source-list turn: %s vs %s", h0First, h0Last)
	}

	// The post-search request carries the numbered source list in its tail hint.
	tail := last.Messages[len(last.Messages)-1]
	if tail.Role != llm.RoleUser {
		t.Fatalf("tail hint role = %q, want user", tail.Role)
	}
	if !strings.Contains(tail.Content, "<sources>") {
		t.Fatalf("tail hint missing <sources> block, got: %q", tail.Content)
	}
	for _, want := range []string{
		"[1] Rome forecast — https://weather.test/rome",
		"[2] Milan forecast — https://weather.test/milan",
	} {
		if !strings.Contains(tail.Content, want) {
			t.Fatalf("tail hint missing %q, got: %q", want, tail.Content)
		}
	}

	// The first (pre-search) request must NOT carry a source list (registry empty).
	firstTail := first.Messages[len(first.Messages)-1]
	if strings.Contains(firstTail.Content, "<sources>") {
		t.Fatalf("pre-search request leaked a source list: %q", firstTail.Content)
	}

	// The volatile list never appears in messages[0].
	if strings.Contains(last.Messages[0].Content, "weather.test/rome") {
		t.Fatalf("volatile source list leaked into messages[0]")
	}
}

// TestStaticCitationConventionInSystemPrompt (DISP-05): the invariant citation
// convention sentence lives in messages[0] (the system prompt) and is a fixed
// string with no per-turn templating.
func TestStaticCitationConventionInSystemPrompt(t *testing.T) {
	if !strings.Contains(agent.SystemPrompt, "[n]") {
		t.Fatalf("system prompt missing the inline citation convention sentence")
	}
	if !strings.Contains(agent.SystemPrompt, "Cite your sources") {
		t.Fatalf("system prompt missing the citation directive headline")
	}
	// The volatile numbered list shape ("[1] ... — url") must NOT be baked into the
	// static prompt — only the convention. A url-bearing source line in messages[0]
	// would poison the cached prefix.
	if strings.Contains(agent.SystemPrompt, "— http") {
		t.Fatalf("system prompt carries a volatile source line; it must ride the tail-inject only")
	}
}

// TestNonWebTurnEmitsNoSourceList: a turn that calls only non-web tools keeps the
// byte-identical default — no <sources> block is tail-injected (backward-compatible).
func TestNonWebTurnEmitsNoSourceList(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "echo", `{"v":"hi"}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := newAgent(t, fc, llm.Config{})
	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := range fc.Requests {
		tail := fc.Requests[i].Messages[len(fc.Requests[i].Messages)-1]
		if strings.Contains(tail.Content, "<sources>") {
			t.Fatalf("non-web turn request %d emitted a source list: %q", i, tail.Content)
		}
	}
}
