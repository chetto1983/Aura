package agent_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// The notice is user-facing copy, pinned here the way llm_agent_test.go pins the
// truncation notice.
const wantRefusalNotice = "[risposta rifiutata dal modello: content_filter]"

// A refusal is the model's answer, not a hiccup: the turn ends on the first call, with
// no nudge, no retry, no dispatch and no finalize synthesis. The second scripted turn
// is what any of those would consume, so one call means none of them ran.
func TestContentFilterEndsTheTurnWithoutRecovery(t *testing.T) {
	refusedCall := agenttest.MakeToolCall("c1", "echo", `{"v":"one"}`)
	for name, tc := range map[string]struct {
		refused  agenttest.FakeTurn
		discards int
	}{
		"partial text": {agenttest.TextChunks(llm.FinishContentFilter, "Ecco come si ", "fa: primo"), 1},
		"no text":      {agenttest.TextChunks(llm.FinishContentFilter), 0},
		"tool call": {agenttest.FakeTurn{Chunks: []llm.Chunk{
			{ToolCall: &refusedCall}, {FinishReason: llm.FinishContentFilter},
		}}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			fc := agenttest.NewFakeClient(tc.refused, agenttest.TextChunks("stop", "seconda chiamata"))
			evs, err := collect(newAgent(t, fc, llm.Config{}).Run(newIC(t, agent.BudgetOptions{})))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if fc.CallCount() != 1 {
				t.Fatalf("model calls = %d, want exactly 1", fc.CallCount())
			}
			if n := countDiscards(evs); n != tc.discards {
				t.Fatalf("DiscardStreamed events = %d, want %d", n, tc.discards)
			}
			final := evs[len(evs)-1]
			if final.LLMResponse == nil || final.LLMResponse.Content != wantRefusalNotice {
				t.Fatalf("last event = %+v, want the refusal notice as the final answer", final)
			}
			if final.LLMResponse.FinishReason != llm.FinishContentFilter {
				t.Fatalf("final finish = %q, want %q", final.LLMResponse.FinishReason, llm.FinishContentFilter)
			}
		})
	}
}
