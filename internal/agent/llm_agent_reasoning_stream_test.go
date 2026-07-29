package agent_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// TestLlmAgent_ReasoningChunk_StreamOnly (amendment #57): a turn that streams a
// reasoning Chunk then content Chunks emits a redacted reasoning Event by default
// (LLMResponse.Reasoning set, Content empty) and the provider reasoning text NEVER
// appears in the final accumulated answer (stream-only, no persistence leak).
func TestLlmAgent_ReasoningChunk_StreamOnly(t *testing.T) {
	recordingProvider(t)
	const reasoning = "let me think about this"
	fc := agenttest.NewFakeClient(agenttest.FakeTurn{Chunks: []llm.Chunk{
		{Reasoning: reasoning},
		{Text: "La "},
		{Text: "risposta."},
		{FinishReason: "stop"},
	}})
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}

	var sawReasoning bool
	for _, ev := range evs {
		if ev.LLMResponse == nil || ev.LLMResponse.Reasoning == "" {
			continue
		}
		sawReasoning = true
		if ev.LLMResponse.Reasoning != "[reasoning redacted]" {
			t.Errorf("reasoning Event content = %q, want redacted marker", ev.LLMResponse.Reasoning)
		}
		if ev.LLMResponse.Content != "" {
			t.Errorf("reasoning Event must carry empty Content, got %q", ev.LLMResponse.Content)
		}
	}
	if !sawReasoning {
		t.Fatal("no reasoning Event emitted for a streamed reasoning Chunk")
	}

	last := evs[len(evs)-1]
	if last.LLMResponse == nil {
		t.Fatalf("final event missing LLMResponse")
	}
	if !strings.Contains(last.LLMResponse.Content, "La risposta.") {
		t.Errorf("final answer = %q, want the streamed content", last.LLMResponse.Content)
	}
	if strings.Contains(last.LLMResponse.Content, reasoning) {
		t.Errorf("final answer leaked provider reasoning: %q", last.LLMResponse.Content)
	}
	if strings.Contains(last.LLMResponse.Content, reasoning) {
		t.Errorf("reasoning text leaked into the final answer %q (must be stream-only)", last.LLMResponse.Content)
	}
}

func TestLlmAgent_ReasoningChunk_PassthroughWhenShowReasoningEnabled(t *testing.T) {
	recordingProvider(t)
	const reasoning = "let me think about this"
	fc := agenttest.NewFakeClient(agenttest.FakeTurn{Chunks: []llm.Chunk{
		{Reasoning: reasoning},
		{Text: "La "},
		{Text: "risposta."},
		{FinishReason: "stop"},
	}})
	a := newAgent(t, fc, llm.Config{ShowReasoning: true})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}

	var got strings.Builder
	for _, ev := range evs {
		if ev.LLMResponse != nil && ev.LLMResponse.Reasoning != "" {
			got.WriteString(ev.LLMResponse.Reasoning)
			if ev.LLMResponse.Content != "" {
				t.Errorf("reasoning Event must carry empty Content, got %q", ev.LLMResponse.Content)
			}
		}
	}
	if got.String() != reasoning {
		t.Fatalf("ShowReasoning reasoning stream = %q, want %q", got.String(), reasoning)
	}

	last := evs[len(evs)-1]
	if last.LLMResponse == nil {
		t.Fatalf("final event missing LLMResponse")
	}
	if strings.Contains(last.LLMResponse.Content, reasoning) {
		t.Errorf("reasoning text leaked into the final answer %q (must be stream-only)", last.LLMResponse.Content)
	}
}
