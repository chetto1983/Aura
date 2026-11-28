package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// routeClient answers each request with the next scripted turn (a plain "Fatto." once
// the script runs out) and records every request it was sent.
type routeClient struct {
	mu       sync.Mutex
	turns    []modelRoundTurn
	requests []llm.Request
}

func (c *routeClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	turn := modelRoundTextTurn("stop", "Fatto.")
	if n := len(c.requests); n < len(c.turns) {
		turn = c.turns[n]
	}
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	ch := make(chan llm.Chunk, len(turn.chunks))
	for _, chunk := range turn.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func anthropicRouteConfig() llm.Config {
	return llm.Config{Model: "claude-test", Provider: llm.AnthropicProvider, BaseURL: llm.AnthropicBaseURL,
		TotalTimeoutSec: 30, MaxTokens: 4096}
}

func openrouterRouteConfig() llm.Config {
	return llm.Config{Model: "deepseek/deepseek-v4-flash", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		TotalTimeoutSec: 30, MaxTokens: 4096}
}

type routeRun struct {
	cfg        llm.Config
	userTurns  []llm.Message
	override   llm.ReasoningEffort
	classifier tierClassifier
	toolRounds int
}

// run drives one turn: toolRounds rounds that call the probe tool, then a text answer.
func (r routeRun) run(t *testing.T) []llm.Request {
	t.Helper()
	client := &routeClient{}
	for i := range r.toolRounds {
		client.turns = append(client.turns, modelRoundToolTurn(modelRoundToolCall(
			"call-"+string(rune('a'+i)), "model_round_probe", `{}`)))
	}
	userTurns := r.userTurns
	if userTurns == nil {
		userTurns = []llm.Message{{Role: llm.RoleUser, Content: "che ore sono a Cuneo?"}}
	}
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(modelRoundProbeTool{})
	a := NewLlmAgent(LlmAgentConfig{
		Client: client, LLM: r.cfg, Registry: registry, PreviewCap: 2048, RunDir: t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(), UserTurns: userTurns,
		ReasoningOverride: r.override,
	})
	a.classifier = r.classifier
	maxSteps := 10
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget}
	for _, err := range a.Run(ic) {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if len(client.requests) != r.toolRounds+1 {
		t.Fatalf("model calls = %d, want %d", len(client.requests), r.toolRounds+1)
	}
	return client.requests
}

func isRoundHint(m llm.Message) bool {
	return m.Role == llm.RoleUser && strings.HasPrefix(m.Content, "<budget>")
}

func TestHintStaysInHistoryOnTheAnthropicRoute(t *testing.T) {
	reqs := routeRun{cfg: anthropicRouteConfig(), toolRounds: 2}.run(t)
	for i, req := range reqs {
		if last := req.Messages[len(req.Messages)-1]; !isRoundHint(last) {
			t.Fatalf("round %d: last message %+v is not the round's hint", i+1, last)
		}
		if req.StablePrefixMessages != len(req.Messages)-1 {
			t.Fatalf("round %d: StablePrefixMessages = %d, want %d", i+1, req.StablePrefixMessages, len(req.Messages)-1)
		}
		if i == 0 {
			continue
		}
		prev := reqs[i-1].Messages
		if len(req.Messages) <= len(prev) || !reflect.DeepEqual(req.Messages[:len(prev)], prev) {
			t.Fatalf("round %d does not extend round %d byte for byte:\n prev %+v\n got  %+v", i+1, i, prev, req.Messages)
		}
	}
	third := reqs[2].Messages
	for i, earlier := range reqs[:2] {
		at := len(earlier.Messages) - 1
		if !reflect.DeepEqual(third[at], earlier.Messages[at]) {
			t.Fatalf("round 3 position %d = %+v, want hint %d %+v", at, third[at], i+1, earlier.Messages[at])
		}
	}
}

func TestHintIsReplacedEveryRoundOnOpenRouter(t *testing.T) {
	reqs := routeRun{cfg: openrouterRouteConfig(), toolRounds: 2}.run(t)
	third := reqs[2].Messages
	var hints int
	for _, m := range third {
		if isRoundHint(m) {
			hints++
		}
	}
	if hints != 1 || !isRoundHint(third[len(third)-1]) {
		t.Fatalf("round 3 carries %d hints, want only its own, last: %+v", hints, third)
	}
	if reqs[2].StablePrefixMessages != 0 || reqs[2].LoadedHistoryMessages != 0 {
		t.Fatalf("openrouter request carries prefix positions: stable=%d loaded=%d",
			reqs[2].StablePrefixMessages, reqs[2].LoadedHistoryMessages)
	}
}

func TestLoadedHistoryCountExcludesTheCurrentTurn(t *testing.T) {
	stored := []llm.Message{
		{Role: llm.RoleUser, Content: "prima domanda"},
		{Role: llm.RoleAssistant, Content: "prima risposta"},
		{Role: llm.RoleUser, Content: "seconda domanda"},
		{Role: llm.RoleAssistant, Content: "seconda risposta"},
		{Role: llm.RoleUser, Content: "terza domanda"},
		{Role: llm.RoleAssistant, Content: "terza risposta"},
	}
	turns := append(append([]llm.Message(nil), stored...), llm.Message{Role: llm.RoleUser, Content: "e adesso?"})
	const systemMessages = 1
	for _, tc := range []struct {
		name string
		cfg  llm.Config
		want int
	}{
		{"anthropic", anthropicRouteConfig(), len(stored) + systemMessages},
		{"openrouter", openrouterRouteConfig(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, req := range (routeRun{cfg: tc.cfg, userTurns: turns, toolRounds: 2}).run(t) {
				if req.LoadedHistoryMessages != tc.want {
					t.Fatalf("round %d: LoadedHistoryMessages = %d, want %d", i+1, req.LoadedHistoryMessages, tc.want)
				}
			}
		})
	}
}

// The finalize synthesis is one more call on the same prefix: it marks the stored
// history where every round did, so it reads that history from the cache.
func TestFinalizeSynthesisCarriesTheLoadedHistory(t *testing.T) {
	stored := []llm.Message{
		{Role: llm.RoleUser, Content: "prima domanda"},
		{Role: llm.RoleAssistant, Content: "prima risposta"},
	}
	turns := append(append([]llm.Message(nil), stored...), llm.Message{Role: llm.RoleUser, Content: "e adesso?"})
	client := &routeClient{}
	a := NewLlmAgent(LlmAgentConfig{
		Client: client, LLM: anthropicRouteConfig(), Registry: tools.NewRegistry(),
		SessionID: uuid.Must(uuid.NewV7()).String(), UserTurns: turns,
	})
	if _, _, err := a.synthesize(InvocationContext{Ctx: context.Background()}); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	const systemMessages = 1
	if got := client.requests[0].LoadedHistoryMessages; got != len(stored)+systemMessages {
		t.Fatalf("finalize LoadedHistoryMessages = %d, want %d", got, len(stored)+systemMessages)
	}
}

func TestLoadedHistoryIsZeroWithoutAUserMessage(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleSystem, Content: "s"}, {Role: llm.RoleAssistant, Content: "a"}}
	if got := loadedHistoryCount(hist); got != 0 {
		t.Fatalf("loadedHistoryCount = %d, want 0", got)
	}
}

func TestAnthropicRouteHoldsTheComposerEffort(t *testing.T) {
	t.Run("no override sends no effort and asks no classifier", func(t *testing.T) {
		classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.5}, ok: true}
		cfg := anthropicRouteConfig()
		cfg.AdaptiveReasoning = true
		reqs := routeRun{cfg: cfg, classifier: classifier, toolRounds: 1}.run(t)
		if classifier.calls != 0 {
			t.Fatalf("classifier called %d times on the anthropic route", classifier.calls)
		}
		for i, req := range reqs {
			if !req.Reasoning.Empty() {
				t.Fatalf("round %d sent reasoning %+v, want none", i+1, req.Reasoning)
			}
		}
	})
	t.Run("override high holds every round", func(t *testing.T) {
		cfg := anthropicRouteConfig()
		cfg.AdaptiveReasoning = true
		reqs := routeRun{cfg: cfg, override: llm.ReasoningEffortHigh, toolRounds: 2}.run(t)
		for i, req := range reqs {
			if req.Reasoning.Effort != llm.ReasoningEffortHigh {
				t.Fatalf("round %d effort = %q, want high", i+1, req.Reasoning.Effort)
			}
		}
	})
}

func TestModelHookCannotMoveTheStablePrefix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*llm.Request)
	}{
		{"stable_prefix_messages", func(r *llm.Request) { r.StablePrefixMessages = 0 }},
		{"loaded_history_messages", func(r *llm.Request) { r.LoadedHistoryMessages++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := protectedModelRequest()
			req.StablePrefixMessages, req.LoadedHistoryMessages = 1, 1
			original := cloneModelRequest(&req)
			_, err := NewHookManager(modelMutationHook{mutate: tc.mutate}).BeforeModel(context.Background(), &req)
			if err == nil || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("BeforeModel error = %v, want %s rejected", err, tc.name)
			}
			if !reflect.DeepEqual(req, original) {
				t.Fatalf("request not restored:\n got  %+v\n want %+v", req, original)
			}
		})
	}
}

// A command hook sees the request through JSON: one that hands it back unchanged must
// pass, and one that drops or moves the positions must not.
func TestCommandHookRoundTripKeepsThePrefixPositions(t *testing.T) {
	req := protectedModelRequest()
	req.StablePrefixMessages, req.LoadedHistoryMessages = 1, 1
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var echoed llm.Request
	if err := json.Unmarshal(raw, &echoed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := validateCommandModelHookMutation(&req, &echoed); err != nil {
		t.Fatalf("unchanged command rewrite rejected: %v", err)
	}
	echoed.StablePrefixMessages = 0
	if err := validateCommandModelHookMutation(&req, &echoed); err == nil {
		t.Fatal("command rewrite that dropped the stable prefix was accepted")
	}
}
