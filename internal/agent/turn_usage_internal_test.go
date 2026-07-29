package agent

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// The defect this guards: usage used to be a loop variable, so a turn with N LLM calls
// reported the Nth and dropped the rest. Measured on the appliance, 24 assistant turns
// recorded usage for 5 — the twelve blanks were exactly the steps that emitted tool
// calls. Summing is what "what did this turn cost" means: every call in the loop
// re-sends the prefix and is billed for it.
func TestTurnUsageSumsEveryCall(t *testing.T) {
	t.Parallel()
	var acc turnUsage
	acc.add(llm.Usage{PromptTokens: 1000, CompletionTokens: 10, CachedTokens: 500, Cost: new(0.001)})
	acc.add(llm.Usage{PromptTokens: 2000, CompletionTokens: 20, CachedTokens: 1500, Cost: new(0.002)})
	acc.add(llm.Usage{PromptTokens: 300, CompletionTokens: 3, CachedTokens: 0, Cost: new(0.0005)})

	got := acc.total()
	if got.PromptTokens != 3300 || got.CompletionTokens != 33 || got.CachedTokens != 2000 {
		t.Errorf("total = %+v, want prompt 3300 / completion 33 / cached 2000", got)
	}
	if got.Cost == nil {
		t.Fatal("Cost = nil, want the summed provider cost")
	}
	if diff := *got.Cost - 0.0035; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Cost = %v, want 0.0035", *got.Cost)
	}
}

// D-18/D-23 honesty: a turn whose calls all omitted cost must keep the nil so the caller
// falls back to the rate table. A zero here would look authoritative and be a lie.
func TestTurnUsageKeepsCostNilWhenNoCallReportedOne(t *testing.T) {
	t.Parallel()
	var acc turnUsage
	acc.add(llm.Usage{PromptTokens: 10, CompletionTokens: 1})
	acc.add(llm.Usage{PromptTokens: 20, CompletionTokens: 2})

	got := acc.total()
	if got.Cost != nil {
		t.Errorf("Cost = %v, want nil when no call carried a provider cost", *got.Cost)
	}
	if got.PromptTokens != 30 {
		t.Errorf("PromptTokens = %d, want 30 — tokens accumulate even without cost", got.PromptTokens)
	}
}

// A partial sum still returns non-nil: it is the provider's own figure for the calls
// that carried one, and understating a known cost beats discarding it.
func TestTurnUsagePartialCostStillReported(t *testing.T) {
	t.Parallel()
	var acc turnUsage
	acc.add(llm.Usage{PromptTokens: 100, Cost: new(0.004)})
	acc.add(llm.Usage{PromptTokens: 200}) // this call reported none

	got := acc.total()
	if got.Cost == nil || *got.Cost != 0.004 {
		t.Errorf("Cost = %v, want the 0.004 the priced call reported", got.Cost)
	}
}

// The zero accumulator must project onto a zero Usage with a nil Cost — the shape a turn
// that never reached an LLM call has to report.
func TestTurnUsageZeroValue(t *testing.T) {
	t.Parallel()
	if got := (&turnUsage{}).total(); got.PromptTokens != 0 || got.Cost != nil {
		t.Errorf("zero total = %+v, want an empty Usage with nil Cost", got)
	}
}

// finalize is the budget-trip exit, i.e. precisely the turns that made the most calls
// before arriving there. It makes ONE more (synthesis) call, and must report that call
// PLUS everything the turn already spent — not the synthesis alone.
func TestFinalizeReportsTheWholeTurnNotJustTheSynthesisCall(t *testing.T) {
	t.Parallel()
	rc := &recordingClient{chunks: []llm.Chunk{
		{Text: "sintesi"},
		{FinishReason: "stop", Usage: &llm.Usage{
			PromptTokens: 700, CompletionTokens: 7, CachedTokens: 100, Cost: new(0.0007),
		}},
	}}
	a := NewLlmAgent(LlmAgentConfig{
		Client:    rc,
		LLM:       llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 5},
		Registry:  tools.NewRegistry(),
		SessionID: "11111111-1111-1111-1111-111111111111",
	})

	// What the turn had already spent across its earlier calls.
	acc := &turnUsage{}
	acc.add(llm.Usage{PromptTokens: 5000, CompletionTokens: 50, CachedTokens: 4000, Cost: new(0.005)})

	var got *Event
	a.finalize(
		InvocationContext{Ctx: context.Background()},
		[8]byte{}, nil, "req-1", "max_steps", acc,
		func(ev *Event, _ error) bool { got = ev; return true },
	)

	if got == nil {
		t.Fatal("finalize yielded no Event")
	}
	d := got.Actions.StateDelta
	if d["prompt_tokens"] != 5700 {
		t.Errorf("prompt_tokens = %v, want 5700 (5000 already spent + 700 synthesis)", d["prompt_tokens"])
	}
	if d["completion_tokens"] != 57 {
		t.Errorf("completion_tokens = %v, want 57", d["completion_tokens"])
	}
	if d["cache_hit_tokens"] != 4100 {
		t.Errorf("cache_hit_tokens = %v, want 4100", d["cache_hit_tokens"])
	}
	cost, ok := d["cost_usd"].(float64)
	if !ok {
		t.Fatalf("cost_usd missing or not a float: %#v", d["cost_usd"])
	}
	if diff := cost - 0.0057; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost_usd = %v, want 0.0057 — the synthesis alone would be 0.0007", cost)
	}
}
